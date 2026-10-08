// Command upgrade-pipeline-tasks changes the version of a task across matching release pipelines.
package upgradepipelinetasks

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type obj = map[string]interface{}

const usageEpilog = `
Examples:
  upgrade-pipeline-tasks 'Example.Project\TEST' --task PowerShell --to-version 2 --dry-run
  upgrade-pipeline-tasks 'Example.Project\TEST' --task e213ff0f-5d5c-4791-802d-52ea3e7be1f1 --from-version 1 --to-version 2 -y
Arguments:
  --task          Task ID (GUID) or task name/friendly name, as known to the organization.
  --to-version    Major version to move to (the newest non-preview build of it is used, e.g. 2 -> "2.*").
  --from-version  Only tasks currently on this major version.
  --force         Apply even when input compatibility problems are found, or when downgrading.
Before upgrading, the step's configured inputs are compared with the inputs of the target version:
inputs that no longer exist and required inputs without a default value are reported, and such steps
are skipped unless --force is given. Disabled task definitions and missing versions are never used.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("upgrade-pipeline-tasks", flag.ExitOnError)
	task := fs.String("task", "", "Task ID or name")
	to := fs.Int("to-version", 0, "Major version to upgrade to")
	from := fs.Int("from-version", 0, "Only steps currently on this major version")
	force := fs.Bool("force", false, "Apply despite compatibility problems or downgrades")
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
		fmt.Fprintln(os.Stderr, "Upgrades a task to another major version across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: upgrade-pipeline-tasks <target> --task <id|name> --to-version <n> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"task": true, "to-version": true, "from-version": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *task == "" || *to <= 0 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	var (
		once    sync.Once
		catalog azuredevops.TaskCatalog
		loadErr error
	)
	mutate := func(cfg azuredevops.Config, _ string, raw map[string]interface{}) []string {
		once.Do(func() { catalog, loadErr = cfg.ListTaskDefinitions() })
		if loadErr != nil {
			fmt.Printf("  - cannot read task definitions: %v\n", loadErr)
			return nil
		}
		ids := catalog.Resolve(*task)
		name, _ := raw["name"].(string)
		if len(ids) != 1 {
			fmt.Printf("  - skipped %s: --task %q matches %d task definitions\n", name, *task, len(ids))
			return nil
		}
		lines, warnings := upgrade(raw, catalog, ids[0], *from, *to, *force)
		for _, w := range warnings {
			fmt.Printf("  - %s: %s\n", name, w)
		}
		return lines
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Upgraded task "+*task+" to v"+strconv.Itoa(*to), mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// upgrade moves matching steps to the target major version. It returns change lines and skip warnings.
func upgrade(raw obj, catalog azuredevops.TaskCatalog, taskID string, from, to int, force bool) (lines, warnings []string) {
	target, ok := catalog.Latest(taskID, to)
	label := catalog.DisplayName(taskID)
	if !ok {
		return nil, []string{fmt.Sprintf("skipped: %s has no version %d (available: %v)", label, to, catalog.Majors(taskID))}
	}
	if target.Disabled && !force {
		return nil, []string{fmt.Sprintf("skipped: %s version %d is disabled", label, to)}
	}
	stages, _ := raw["environments"].([]interface{})
	for _, s := range stages {
		stage, _ := s.(obj)
		stageName, _ := stage["name"].(string)
		phases, _ := stage["deployPhases"].([]interface{})
		for _, p := range phases {
			phase, _ := p.(obj)
			phaseName, _ := phase["name"].(string)
			tasks, _ := phase["workflowTasks"].([]interface{})
			for _, t := range tasks {
				step, _ := t.(obj)
				if id, _ := step["taskId"].(string); !strings.EqualFold(id, taskID) {
					continue
				}
				spec, _ := step["version"].(string)
				current, parsed := azuredevops.ParseTaskMajor(spec)
				stepName, _ := step["name"].(string)
				where := fmt.Sprintf("[%s / %s / %s]", stageName, phaseName, stepName)
				switch {
				case parsed && current == to:
					continue
				case from > 0 && (!parsed || current != from):
					continue
				case parsed && current > to && !force:
					warnings = append(warnings, fmt.Sprintf("%s skipped: v%d -> v%d is a downgrade (use --force)", where, current, to))
					continue
				}
				if problems := compatibility(step, target); len(problems) > 0 {
					msg := fmt.Sprintf("%s incompatible with v%d: %s", where, to, strings.Join(problems, "; "))
					if !force {
						warnings = append(warnings, msg+" (skipped; use --force)")
						continue
					}
					warnings = append(warnings, msg+" (forced)")
				}
				step["version"] = fmt.Sprintf("%d.*", to)
				lines = append(lines, fmt.Sprintf("%s %s: %s -> %d.*", where, label, firstNonEmpty(spec, "unset"), to))
			}
		}
	}
	return lines, warnings
}

// compatibility lists reasons the step's configured inputs do not fit the target task version.
func compatibility(step obj, target azuredevops.TaskDefinition) []string {
	known := map[string]bool{}
	for _, in := range target.Inputs {
		known[in.Name] = true
	}
	inputs, _ := step["inputs"].(obj)
	var problems []string
	var removed []string
	for name, value := range inputs {
		if v, _ := value.(string); v != "" && !known[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	for _, name := range removed {
		problems = append(problems, fmt.Sprintf("input %q does not exist", name))
	}
	for _, in := range target.Inputs {
		if v, _ := inputs[in.Name].(string); in.Required && in.DefaultValue == "" && v == "" {
			problems = append(problems, fmt.Sprintf("required input %q has no value", in.Name))
		}
	}
	return problems
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
