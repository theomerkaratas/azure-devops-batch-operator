// Command copy-pipeline-stage copies one complete stage from a source pipeline into target pipelines.
package copypipelinestage

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
	"github.com/omerkaratas/azure-devops-go-automations/internal/stageutil"
)

const usageEpilog = `
Examples:
  copy-pipeline-stage 'Example.Project\TEST' --source 'Example.Project\GOLDEN\Deploy' --stage Production --dry-run
  copy-pipeline-stage 'Example.Project\TEST' --source 'Example.Project\GOLDEN\Deploy' --stage Production --on-existing replace -y
  copy-pipeline-stage 'Example.Project\TEST' --source 'Example.Project\GOLDEN\Deploy' --stage Production --on-existing rename --new-name Production-copy -y
The whole stage is copied: jobs, tasks, variables, conditions, approvals and retention.
When the target already has a stage with that name (--on-existing):
  skip     Leave the target unchanged (default).
  replace  Overwrite the stage in place, keeping its position and secret variable values.
  rename   Add the copy under --new-name (default: "<stage>-copy").
New stages go to the end, or after --after. Triggers that point at stages missing in the target are
dropped (falling back to the release start). Masked secret values cannot be copied and must be set afterwards.
`

type options struct {
	stage, onExisting, newName, after string
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("copy-pipeline-stage", flag.ExitOnError)
	source := fs.String("source", "", "Full path of the pipeline to copy the stage from")
	var o options
	fs.StringVar(&o.stage, "stage", "", "Name of the stage to copy")
	fs.StringVar(&o.onExisting, "on-existing", "skip", "If the target has the stage: skip, replace or rename")
	fs.StringVar(&o.newName, "new-name", "", "Name for the copy (default with rename: <stage>-copy)")
	fs.StringVar(&o.after, "after", "", "Place a new stage after this target stage (default: end)")
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
		fmt.Fprintln(os.Stderr, "Copies one complete stage from a source pipeline into target pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: copy-pipeline-stage <target> --source <pipeline> --stage <name> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"source": true, "stage": true, "on-existing": true, "new-name": true, "after": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *source == "" || o.stage == "" {
		fs.Usage()
		os.Exit(2)
	}
	o.onExisting = strings.ToLower(o.onExisting)
	if o.onExisting != "skip" && o.onExisting != "replace" && o.onExisting != "rename" {
		fmt.Fprintln(os.Stderr, "Error: --on-existing must be one of: skip, replace, rename")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *source, *filter, *level, *dryRun, *yes, o); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, source, filter, level string, dryRun, autoYes bool, o options) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	srcProject, srcPath, srcName, err := azuredevops.ParsePipelinePath(source)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	targetProject := strings.Split(strings.ReplaceAll(target, "/", `\`), `\`)[0]
	if !strings.EqualFold(targetProject, srcProject) {
		return fmt.Errorf("stages cannot be copied across projects because queue and variable-group IDs are project-specific; use a source pipeline in %s", targetProject)
	}
	def, err := cfg.FindDefinition(srcProject, srcPath, srcName)
	if err != nil {
		return err
	}
	srcRaw, err := cfg.GetDefinitionDetailRaw(srcProject, def.ID)
	if err != nil {
		return fmt.Errorf("read source pipeline: %w", err)
	}
	srcStages := stageutil.Stages(srcRaw)
	i := stageutil.Index(srcStages, o.stage)
	if i < 0 {
		return fmt.Errorf("stage %q not found in %s", o.stage, source)
	}
	srcStage := srcStages[i]
	mutate := func(_ azuredevops.Config, project string, raw map[string]interface{}) []string {
		if project == srcProject && numericID(raw["id"]) == def.ID {
			return nil
		}
		lines, err := copyStage(raw, srcStage, o)
		if err != nil {
			name, _ := raw["name"].(string)
			fmt.Printf("  - skipped %s: %v\n", name, err)
			return nil
		}
		return lines
	}
	opts := batchupdate.Options{Target: target, Filter: filter, Level: level, DryRun: dryRun, AutoYes: autoYes}
	return batchupdate.Run(opts, fmt.Sprintf("Copied stage %s from %s", o.stage, source), mutate)
}

// copyStage copies srcStage into raw according to the --on-existing policy.
func copyStage(raw, srcStage map[string]interface{}, o options) ([]string, error) {
	stages := stageutil.Stages(raw)
	name := stageutil.Name(srcStage)
	if o.newName != "" && o.onExisting != "replace" {
		name = o.newName
	}
	existing := stageutil.Index(stages, stageutil.Name(srcStage))
	copyOf, _ := stageutil.Clone(srcStage).(stageutil.Map)
	var line string
	switch {
	case existing >= 0 && o.onExisting == "replace":
		dest := stages[existing]
		stageutil.RebindIDs(copyOf, dest)
		keepSecrets(copyOf, dest)
		copyOf["rank"] = dest["rank"]
		stages[existing] = copyOf
		line = fmt.Sprintf("replace stage %q", stageutil.Name(srcStage))
	case existing >= 0 && o.onExisting == "skip":
		return nil, nil
	default:
		if existing >= 0 && o.newName == "" {
			name = stageutil.Name(srcStage) + "-copy"
		}
		if stageutil.Index(stages, name) >= 0 {
			return nil, fmt.Errorf("stage %q already exists", name)
		}
		stageutil.ResetIDs(copyOf)
		copyOf["name"] = name
		at := len(stages)
		if o.after != "" {
			j := stageutil.Index(stages, o.after)
			if j < 0 {
				return nil, fmt.Errorf("--after stage %q not found", o.after)
			}
			at = j + 1
		}
		stages = stageutil.Insert(stages, copyOf, at)
		line = fmt.Sprintf("add copy of stage %q as %q at position %d", stageutil.Name(srcStage), name, at+1)
	}
	known := map[string]bool{}
	for _, s := range stages {
		known[strings.ToLower(stageutil.Name(s))] = true
	}
	stageutil.PruneDependencies(copyOf, known)
	if err := stageutil.Validate(stages); err != nil {
		return nil, err
	}
	if secrets := countSecrets(copyOf); secrets > 0 {
		line += fmt.Sprintf(" (%d secret variable(s) need values set)", secrets)
	}
	stageutil.Save(raw, stages)
	return []string{line}, nil
}

// keepSecrets preserves the destination's values for secret variables, which the source masks.
func keepSecrets(stage, dest map[string]interface{}) {
	vars, _ := stage["variables"].(map[string]interface{})
	destVars, _ := dest["variables"].(map[string]interface{})
	for key, value := range vars {
		v, _ := value.(map[string]interface{})
		if secret, _ := v["isSecret"].(bool); secret {
			if existing, ok := destVars[key]; ok {
				vars[key] = stageutil.Clone(existing)
			}
		}
	}
}

func countSecrets(stage map[string]interface{}) int {
	n := 0
	vars, _ := stage["variables"].(map[string]interface{})
	for _, value := range vars {
		v, _ := value.(map[string]interface{})
		if secret, _ := v["isSecret"].(bool); secret && v["value"] == nil {
			n++
		}
	}
	return n
}

func numericID(value interface{}) int {
	if f, ok := value.(float64); ok {
		return int(f)
	}
	return 0
}
