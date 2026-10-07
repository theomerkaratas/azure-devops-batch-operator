// Command manage-pipeline-stages adds, clones, renames, removes or reorders stages in release pipelines.
package managepipelinestages

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
  manage-pipeline-stages 'Example.Project\TEST' --action add --stage QA --after Dev --dry-run
  manage-pipeline-stages 'Example.Project\TEST' --action clone --stage Prod --new-name Prod-EU -y
  manage-pipeline-stages 'Example.Project\TEST' --action rename --stage QA --new-name Staging -y
  manage-pipeline-stages 'Example.Project\TEST' --action remove --stage QA --rewire -y
  manage-pipeline-stages 'Example.Project\TEST' --action reorder --stage Staging --position 1 --dry-run
Actions:
  add      Adds an empty stage (automated approvals, default retention). Needs --stage.
  clone    Duplicates --stage as --new-name (keeps its jobs, tasks, variables, approvals).
  rename   Renames --stage to --new-name and updates stages that depend on it.
  remove   Removes --stage. Fails if other stages depend on it, unless --rewire is given.
  reorder  Moves --stage to --after <stage> or --position <n>.
Placement and dependencies:
  --after / --position  Where add, clone and reorder put the stage (default: end; clone: after the source).
  --depends-on          Comma-separated stages the stage waits for. Default for add: the --after stage,
                        otherwise the release start. Clone keeps the source's triggers unless set.
Pipelines where the change is impossible or would break stage order are reported and left untouched.
`

type options struct {
	action, stage, newName, after, dependsOn string
	position                                 int
	rewire                                   bool
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("manage-pipeline-stages", flag.ExitOnError)
	var o options
	fs.StringVar(&o.action, "action", "", "add, clone, rename, remove or reorder")
	fs.StringVar(&o.stage, "stage", "", "Stage name the action applies to")
	fs.StringVar(&o.newName, "new-name", "", "New stage name (clone, rename)")
	fs.StringVar(&o.after, "after", "", "Place the stage after this stage")
	fs.IntVar(&o.position, "position", 0, "Place the stage at this 1-based position")
	fs.StringVar(&o.dependsOn, "depends-on", "", "Comma-separated stages the stage waits for")
	fs.BoolVar(&o.rewire, "rewire", false, "On remove, point dependent stages at the removed stage's own dependencies")
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
		fmt.Fprintln(os.Stderr, "Adds, clones, renames, removes or reorders stages across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: manage-pipeline-stages <target> --action <action> --stage <name> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"action": true, "stage": true, "new-name": true, "after": true, "position": true, "depends-on": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := o.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		lines, err := apply(raw, o)
		if err != nil {
			name, _ := raw["name"].(string)
			fmt.Printf("  - skipped %s: %v\n", name, err)
			return nil
		}
		return lines
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Stage "+o.action+": "+o.stage, mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func (o *options) validate() error {
	o.action = strings.ToLower(o.action)
	switch o.action {
	case "add", "clone", "rename", "remove", "reorder":
	default:
		return fmt.Errorf("--action must be one of: add, clone, rename, remove, reorder")
	}
	if o.stage == "" {
		return fmt.Errorf("--stage is required")
	}
	if (o.action == "clone" || o.action == "rename") && o.newName == "" {
		return fmt.Errorf("--new-name is required for %s", o.action)
	}
	if o.action == "reorder" && o.after == "" && o.position == 0 {
		return fmt.Errorf("reorder needs --after or --position")
	}
	if o.after != "" && o.position != 0 {
		return fmt.Errorf("use either --after or --position, not both")
	}
	if o.position < 0 {
		return fmt.Errorf("--position must be 1 or greater")
	}
	return nil
}

func splitNames(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// apply performs the action on one definition and returns the change lines.
func apply(raw map[string]interface{}, o options) ([]string, error) {
	stages := stageutil.Stages(raw)
	idx := stageutil.Index(stages, o.stage)
	var line string
	switch o.action {
	case "add":
		if idx >= 0 {
			return nil, fmt.Errorf("stage %q already exists", o.stage)
		}
		var owner interface{}
		if len(stages) > 0 {
			owner = stages[0]["owner"]
		}
		stage := stageutil.NewStage(o.stage, owner)
		at, err := placement(stages, o, len(stages))
		if err != nil {
			return nil, err
		}
		deps := splitNames(o.dependsOn)
		if len(deps) == 0 && o.after != "" {
			deps = []string{stages[stageutil.Index(stages, o.after)]["name"].(string)}
		}
		stageutil.SetDependencies(stage, deps)
		stages = stageutil.Insert(stages, stage, at)
		line = fmt.Sprintf("add stage %q at position %d", o.stage, at+1)
	case "clone":
		if idx < 0 {
			return nil, fmt.Errorf("stage %q not found", o.stage)
		}
		if stageutil.Index(stages, o.newName) >= 0 {
			return nil, fmt.Errorf("stage %q already exists", o.newName)
		}
		stage, _ := stageutil.Clone(stages[idx]).(stageutil.Map)
		stageutil.ResetIDs(stage)
		stage["name"] = o.newName
		at, err := placement(stages, o, idx+1)
		if err != nil {
			return nil, err
		}
		if deps := splitNames(o.dependsOn); len(deps) > 0 {
			stageutil.SetDependencies(stage, deps)
		} else if o.after != "" {
			stageutil.SetDependencies(stage, []string{stageutil.Name(stages[stageutil.Index(stages, o.after)])})
		}
		stages = stageutil.Insert(stages, stage, at)
		line = fmt.Sprintf("clone stage %q as %q at position %d", o.stage, o.newName, at+1)
	case "rename":
		if idx < 0 {
			return nil, fmt.Errorf("stage %q not found", o.stage)
		}
		if j := stageutil.Index(stages, o.newName); j >= 0 && j != idx {
			return nil, fmt.Errorf("stage %q already exists", o.newName)
		}
		if stageutil.Name(stages[idx]) == o.newName {
			return nil, nil
		}
		old := stageutil.Name(stages[idx])
		stageutil.RenameDependency(stages, old, o.newName)
		stages[idx]["name"] = o.newName
		line = fmt.Sprintf("rename stage %q to %q", old, o.newName)
	case "remove":
		if idx < 0 {
			return nil, fmt.Errorf("stage %q not found", o.stage)
		}
		name := stageutil.Name(stages[idx])
		dependents := stageutil.Dependents(stages, name)
		if len(dependents) > 0 && !o.rewire {
			names := make([]string, len(dependents))
			for i, d := range dependents {
				names[i] = stageutil.Name(d)
			}
			return nil, fmt.Errorf("stages depend on %q (%s); use --rewire", name, strings.Join(names, ", "))
		}
		upstream := stageutil.Dependencies(stages[idx])
		for _, d := range dependents {
			var next []string
			for _, dep := range stageutil.Dependencies(d) {
				if strings.EqualFold(dep, name) {
					next = appendUnique(next, upstream...)
				} else {
					next = appendUnique(next, dep)
				}
			}
			stageutil.SetDependencies(d, next)
		}
		stages = stageutil.Remove(stages, idx)
		line = fmt.Sprintf("remove stage %q", name)
		if len(dependents) > 0 {
			line += fmt.Sprintf(" (rewired %d dependent stage(s))", len(dependents))
		}
	case "reorder":
		if idx < 0 {
			return nil, fmt.Errorf("stage %q not found", o.stage)
		}
		if strings.EqualFold(o.after, o.stage) {
			return nil, fmt.Errorf("--after must be a different stage")
		}
		stage := stages[idx]
		rest := stageutil.Remove(append([]stageutil.Map(nil), stages...), idx)
		at, err := placement(rest, o, len(rest))
		if err != nil {
			return nil, err
		}
		if deps := splitNames(o.dependsOn); len(deps) > 0 {
			stageutil.SetDependencies(stage, deps)
		}
		stages = stageutil.Insert(rest, stage, at)
		if at == idx {
			if len(splitNames(o.dependsOn)) == 0 {
				return nil, nil
			}
		}
		line = fmt.Sprintf("move stage %q from position %d to %d", o.stage, idx+1, at+1)
	}
	if err := stageutil.Validate(stages); err != nil {
		return nil, err
	}
	stageutil.Save(raw, stages)
	return []string{line}, nil
}

// placement resolves --after/--position into a 0-based insert index, or returns def.
func placement(stages []stageutil.Map, o options, def int) (int, error) {
	switch {
	case o.after != "":
		i := stageutil.Index(stages, o.after)
		if i < 0 {
			return 0, fmt.Errorf("--after stage %q not found", o.after)
		}
		return i + 1, nil
	case o.position > 0:
		if o.position > len(stages)+1 {
			return 0, fmt.Errorf("--position %d is beyond the end of the pipeline", o.position)
		}
		return o.position - 1, nil
	}
	return def, nil
}

func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range list {
			if strings.EqualFold(existing, item) {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
}
