// Command update-pipeline-variables bulk-sets or removes variables of Azure DevOps release
// pipelines under a given path.
package updatepipelinevariables

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  # Preview first:
  update-pipeline-variables 'Example.Project\TEST\CONFIG' --set Env=test --dry-run
  # Set a pipeline variable, remove another, after confirming:
  update-pipeline-variables 'Example.Project\TEST\CONFIG' --set Env=test --remove OldVar
  # Set a stage-level variable on one stage only:
  update-pipeline-variables 'Example.Project\TEST\CONFIG' --scope stage --stage Development --set Url=http://x -y
Arguments:
  target     Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --set      NAME=VALUE to add or update. May be given multiple times.
  --secret   Mark the variables given with --set as secret.
  --remove   Variable name to remove. May be given multiple times.
             At least one of --set or --remove is required.
  --scope    Optional. Where the variables live (default: pipeline).
             Choices: pipeline | stage
  --stage    Optional. With --scope stage, only touch the given stage name.
  --filter   Optional. Only pipelines whose name contains this text (case-insensitive).
  --level    Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run  Lists the changes without saving anything.
  -y, --yes  Skips the confirmation prompt.
`

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type assignment struct{ name, value string }

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-variables", flag.ExitOnError)
	sets, removes := multiFlag{}, multiFlag{}
	fs.Var(&sets, "set", "NAME=VALUE to add or update. May be given multiple times.")
	fs.Var(&removes, "remove", "Variable name to remove. May be given multiple times.")
	secret := fs.Bool("secret", false, "Mark variables given with --set as secret")
	scope := fs.String("scope", "pipeline", "Where the variables live: pipeline or stage (default: pipeline)")
	stage := fs.String("stage", "", "With --scope stage, only touch the given stage name")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists the changes without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-sets or removes variables of release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: update-pipeline-variables <target> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"set": true, "remove": true, "scope": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if len(sets) == 0 && len(removes) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify at least one --set or --remove.")
		os.Exit(2)
	}
	if *scope != "pipeline" && *scope != "stage" {
		fmt.Fprintln(os.Stderr, "Error: --scope must be one of: pipeline, stage")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	var assignments []assignment
	for _, s := range sets {
		name, value, ok := strings.Cut(s, "=")
		if !ok || strings.TrimSpace(name) == "" {
			fmt.Fprintf(os.Stderr, "Error: --set expects NAME=VALUE, got %q\n", s)
			os.Exit(2)
		}
		assignments = append(assignments, assignment{strings.TrimSpace(name), value})
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		if *scope == "pipeline" {
			return applyVariables(raw, "pipeline", assignments, *secret, removes)
		}
		var lines []string
		batchupdate.EachEnvironment(raw, *stage, func(name string, env map[string]interface{}) {
			lines = append(lines, applyVariables(env, "stage "+name, assignments, *secret, removes)...)
		})
		return lines
	}
	if err := batchupdate.Run(opts, "Automated variables update", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func display(v map[string]interface{}) string {
	if secret, _ := v["isSecret"].(bool); secret {
		return "***"
	}
	s, _ := v["value"].(string)
	return s
}

// applyVariables edits owner["variables"] in place and describes each change.
func applyVariables(owner map[string]interface{}, label string, sets []assignment, secret bool, removes []string) []string {
	vars, _ := owner["variables"].(map[string]interface{})
	var lines []string
	for _, a := range sets {
		cur, exists := vars[a.name].(map[string]interface{})
		shown := a.value
		if secret {
			shown = "***"
		}
		switch {
		case !exists:
			if vars == nil {
				vars = map[string]interface{}{}
				owner["variables"] = vars
			}
			vars[a.name] = map[string]interface{}{"value": a.value, "isSecret": secret}
			lines = append(lines, fmt.Sprintf("[%s] + %s = %s", label, a.name, shown))
		default:
			wasSecret, _ := cur["isSecret"].(bool)
			oldValue, _ := cur["value"].(string)
			// A secret's current value is never returned, so a set on it always counts as a change.
			if !wasSecret && !secret && oldValue == a.value {
				continue
			}
			old := display(cur)
			cur["value"] = a.value
			if secret {
				cur["isSecret"] = true
			}
			lines = append(lines, fmt.Sprintf("[%s] ~ %s: %s -> %s", label, a.name, old, shown))
		}
	}
	for _, name := range removes {
		if cur, ok := vars[name].(map[string]interface{}); ok {
			lines = append(lines, fmt.Sprintf("[%s] - %s (was %s)", label, name, display(cur)))
			delete(vars, name)
		}
	}
	sort.Strings(lines)
	return lines
}
