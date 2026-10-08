// Command update-pipeline-stage-triggers changes when stages start (after release, after stages, or manually).
package updatepipelinestagetriggers

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
  update-pipeline-stage-triggers 'Example.Project\TEST' --trigger sequential --dry-run
  update-pipeline-stage-triggers 'Example.Project\TEST' --trigger after-stages --stage Prod --after QA,Perf -y
  update-pipeline-stage-triggers 'Example.Project\TEST' --trigger manual --stage Prod -y
Triggers:
  after-release  Stage starts automatically as soon as a release is created.
  after-stages   Stage starts after all stages in --after succeed.
  manual         Stage starts only when someone deploys it.
  sequential     Every selected stage follows the stage before it in pipeline order
                 (the first one starts after the release).
--stage limits the change to the comma-separated stages (default: all). Pipelines where the result
would be invalid (missing stage, dependency order) are reported and left untouched.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-stage-triggers", flag.ExitOnError)
	trigger := fs.String("trigger", "", "after-release, after-stages, manual or sequential")
	stageList := fs.String("stage", "", "Comma-separated stages to change (default: all)")
	afterList := fs.String("after", "", "Comma-separated stages to wait for (after-stages)")
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
		fmt.Fprintln(os.Stderr, "Changes when stages start across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-stage-triggers <target> --trigger <kind> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"trigger": true, "stage": true, "after": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	kind := strings.ToLower(*trigger)
	after := splitNames(*afterList)
	if fs.NArg() != 1 || kind == "" {
		fs.Usage()
		os.Exit(2)
	}
	switch {
	case kind != "after-release" && kind != "after-stages" && kind != "manual" && kind != "sequential":
		fmt.Fprintln(os.Stderr, "Error: --trigger must be one of: after-release, after-stages, manual, sequential")
		os.Exit(2)
	case kind == "after-stages" && len(after) == 0:
		fmt.Fprintln(os.Stderr, "Error: --trigger after-stages needs --after")
		os.Exit(2)
	case kind != "after-stages" && len(after) > 0:
		fmt.Fprintln(os.Stderr, "Error: --after is only valid with --trigger after-stages")
		os.Exit(2)
	case !batchupdate.ValidWriteLevel(*level):
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	selected := splitNames(*stageList)
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		lines, err := apply(raw, kind, selected, after)
		if err != nil {
			name, _ := raw["name"].(string)
			fmt.Printf("  - skipped %s: %v\n", name, err)
			return nil
		}
		return lines
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Updated stage triggers: "+kind, mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
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

func describe(kind string, deps []string) string {
	if kind == stageutil.TriggerStages {
		return "after " + strings.Join(deps, ", ")
	}
	return kind
}

// apply sets the trigger on the selected stages of one definition.
func apply(raw map[string]interface{}, kind string, selected, after []string) ([]string, error) {
	stages := stageutil.Stages(raw)
	for _, name := range append(append([]string(nil), selected...), after...) {
		if stageutil.Index(stages, name) < 0 {
			return nil, fmt.Errorf("stage %q not found", name)
		}
	}
	chosen := func(s stageutil.Map) bool {
		if len(selected) == 0 {
			return true
		}
		return contains(selected, stageutil.Name(s))
	}
	var lines []string
	for i, s := range stages {
		if !chosen(s) {
			continue
		}
		var wantKind string
		var wantDeps []string
		switch kind {
		case "after-release":
			wantKind = stageutil.TriggerRelease
		case "manual":
			wantKind = stageutil.TriggerManual
		case "after-stages":
			wantKind = stageutil.TriggerStages
			for _, a := range after {
				if !strings.EqualFold(a, stageutil.Name(s)) {
					wantDeps = append(wantDeps, stageutil.Name(stages[stageutil.Index(stages, a)]))
				}
			}
			if len(wantDeps) == 0 {
				continue
			}
		case "sequential":
			if i == 0 {
				wantKind = stageutil.TriggerRelease
			} else {
				wantKind, wantDeps = stageutil.TriggerStages, []string{stageutil.Name(stages[i-1])}
			}
		}
		curKind, curDeps := stageutil.TriggerOf(s)
		if curKind == wantKind && stageutil.SameSet(curDeps, wantDeps) {
			continue
		}
		switch wantKind {
		case stageutil.TriggerManual:
			stageutil.SetManual(s)
		default:
			stageutil.SetDependencies(s, wantDeps)
		}
		lines = append(lines, fmt.Sprintf("[%s] trigger: %s -> %s", stageutil.Name(s), describe(curKind, curDeps), describe(wantKind, wantDeps)))
	}
	if len(lines) == 0 {
		return nil, nil
	}
	if err := stageutil.Validate(stages); err != nil {
		return nil, err
	}
	stageutil.Save(raw, stages)
	return lines, nil
}

func contains(list []string, name string) bool {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
