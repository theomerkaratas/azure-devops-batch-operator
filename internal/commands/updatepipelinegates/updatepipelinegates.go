// Command update-pipeline-gates standardizes pre-deployment and post-deployment gates (REST API
// checks, Azure Functions, monitoring queries, evaluation intervals, timeout behavior, ...)
// across release pipelines, copying them from a reference pipeline's name-matched stages.
package updatepipelinegates

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  update-pipeline-gates 'Example.Project\TEST' --reference 'Example.Project\GOLDEN\Deploy' --dry-run
  update-pipeline-gates 'Example.Project\TEST' --reference 'Example.Project\GOLDEN\Deploy' --phase post --stage Production -y
Arguments:
  target       Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --reference  Required. Full path of the pipeline whose gates are the standard to apply.
  --phase      pre, post, or both (default: both).
  --stage      Only standardize the named stage (default: every stage present on both sides).
  --filter     Only pipeline names containing this text (case-insensitive).
  --level      PAT authorization level: read-write or manage (default: read-write).
  --dry-run    Lists planned changes without saving.
  -y, --yes    Skips the confirmation prompt.

Gate tasks (REST API checks, Azure Function checks, monitoring queries, etc.), evaluation
options (sampling interval, stabilization time, minimum success duration) and the overall
timeout are all copied verbatim from the reference stage, including task inputs such as service
connection or endpoint references, so the reference pipeline must live in the same project as any
target pipeline whose task inputs are project-specific.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-gates", flag.ExitOnError)
	reference := fs.String("reference", "", "Full path of the pipeline whose gates are the standard to apply")
	phase := fs.String("phase", "both", "Which gate list to standardize: pre, post, or both")
	stage := fs.String("stage", "", "Only standardize the named stage")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists planned changes without saving")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Standardizes pre/post-deployment gates across release pipelines from a reference pipeline.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-gates <target> --reference <pipeline> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"reference": true, "phase": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *reference == "" {
		fs.Usage()
		os.Exit(2)
	}
	phases, err := parsePhases(*phase)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *reference, *stage, *filter, *level, *dryRun, *yes, phases); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

var phaseKeys = map[string]string{"pre": "preDeploymentGates", "post": "postDeploymentGates"}

func parsePhases(value string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pre":
		return []string{"pre"}, nil
	case "post":
		return []string{"post"}, nil
	case "both", "":
		return []string{"pre", "post"}, nil
	default:
		return nil, fmt.Errorf("--phase must be one of: pre, post, both")
	}
}

func run(target, reference, stage, filter, level string, dryRun, autoYes bool, phases []string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	refProject, refPath, refName, err := azuredevops.ParsePipelinePath(reference)
	if err != nil {
		return fmt.Errorf("reference: %w", err)
	}
	refDefinition, err := cfg.FindDefinition(refProject, refPath, refName)
	if err != nil {
		return err
	}
	refRaw, err := cfg.GetDefinitionDetailRaw(refProject, refDefinition.ID)
	if err != nil {
		return fmt.Errorf("read reference pipeline: %w", err)
	}
	refStages := stagesByName(refRaw)

	mutate := func(_ azuredevops.Config, project string, raw map[string]interface{}) []string {
		if project == refProject && batchupdate.Int(raw, "id") == refDefinition.ID {
			return nil
		}
		return standardize(raw, stage, phases, refStages)
	}
	opts := batchupdate.Options{Target: target, Filter: filter, Level: level, DryRun: dryRun, AutoYes: autoYes}
	return batchupdate.Run(opts, "Standardized deployment gates from "+reference, mutate)
}

func standardize(raw map[string]interface{}, stageFilter string, phases []string, refStages map[string]map[string]interface{}) []string {
	var changes []string
	batchupdate.EachEnvironment(raw, stageFilter, func(name string, env map[string]interface{}) {
		refStage, ok := refStages[strings.ToLower(name)]
		if !ok {
			return
		}
		for _, phase := range phases {
			key := phaseKeys[phase]
			wanted := clone(refStage[key])
			if !reflect.DeepEqual(env[key], wanted) {
				env[key] = wanted
				changes = append(changes, fmt.Sprintf("[stage %s / %s] gates standardized", name, phase))
			}
		}
	})
	sort.Strings(changes)
	return changes
}

func stagesByName(raw map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	envs, _ := raw["environments"].([]interface{})
	for _, e := range envs {
		env, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := env["name"].(string)
		out[strings.ToLower(name)] = env
	}
	return out
}

// clone deep-copies a JSON-shaped value so a gate block taken from the reference pipeline is
// never shared with (and mutated through) the definitions it gets copied into.
func clone(value interface{}) interface{} {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}
