// Command detect-deprecated-tasks reports tasks that are deprecated, disabled, missing from the
// organization, or pinned to an unavailable version.
package detectdeprecatedtasks

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type obj = map[string]interface{}

const usageEpilog = `
Examples:
  detect-deprecated-tasks 'Example.Project\TEST'
  detect-deprecated-tasks 'Example.Project' --include-outdated --fail-on-findings
Findings (enabled steps only):
  MISSING      The task is not installed in the organization.
  UNSUPPORTED  The step uses a major version the organization does not have.
  DISABLED     The task version is disabled and will not run.
  DEPRECATED   The task version is marked deprecated.
  OUTDATED     A newer major version exists (only with --include-outdated).
With --fail-on-findings the exit code is 1 when any finding other than OUTDATED exists.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("detect-deprecated-tasks", flag.ExitOnError)
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	outdated := fs.Bool("include-outdated", false, "Also report steps with a newer major version available")
	fail := fs.Bool("fail-on-findings", false, "Exit with status 1 when findings other than OUTDATED exist")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Reports deprecated, disabled, missing or unsupported tasks in release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: detect-deprecated-tasks <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *level, *outdated, *fail); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

type finding struct{ kind, text string }

func run(target, filter, level string, outdated, fail bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under `%s`.\n", target)
		return nil
	}
	catalog, err := cfg.ListTaskDefinitions()
	if err != nil {
		return fmt.Errorf("read task definitions: %w", err)
	}
	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))
	affected, risky, notes := 0, 0, 0
	for _, d := range defs {
		raw, err := cfg.GetDefinitionDetailRaw(project, d.ID)
		if err != nil {
			return fmt.Errorf("read %s: %w", d.Name, err)
		}
		findings := inspect(raw, catalog, outdated)
		if len(findings) == 0 {
			continue
		}
		affected++
		fmt.Printf("\n%s\\%s (id=%d)\n", strings.TrimRight(d.Path, `\`), d.Name, d.ID)
		for _, f := range findings {
			fmt.Printf("  [%s] %s\n", f.kind, f.text)
			if f.kind == "OUTDATED" {
				notes++
			} else {
				risky++
			}
		}
	}
	fmt.Printf("\nCheck complete: %d pipeline(s), %d with findings, %d at risk, %d outdated.\n", len(defs), affected, risky, notes)
	if fail && risky > 0 {
		return fmt.Errorf("found %d task problem(s)", risky)
	}
	return nil
}

// inspect returns the findings for the enabled steps of one release definition.
func inspect(raw obj, catalog azuredevops.TaskCatalog, outdated bool) []finding {
	var out []finding
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
				if enabled, ok := step["enabled"].(bool); ok && !enabled {
					continue
				}
				id, _ := step["taskId"].(string)
				if id == "" {
					continue
				}
				name, _ := step["name"].(string)
				where := fmt.Sprintf("%s / %s / %s", stageName, phaseName, name)
				spec, _ := step["version"].(string)
				if !catalog.Exists(id) {
					out = append(out, finding{"MISSING", fmt.Sprintf("%s: task %s (%s) is not installed in the organization", where, id, spec)})
					continue
				}
				label := catalog.DisplayName(id)
				majors := catalog.Majors(id)
				major, parsed := azuredevops.ParseTaskMajor(spec)
				def, ok := catalog.Latest(id, major)
				if !parsed || !ok {
					out = append(out, finding{"UNSUPPORTED", fmt.Sprintf("%s: %s version %q is not available (available majors: %v)", where, label, spec, majors)})
					continue
				}
				switch {
				case def.Disabled:
					out = append(out, finding{"DISABLED", fmt.Sprintf("%s: %s v%d is disabled", where, label, major)})
				case def.Deprecated:
					out = append(out, finding{"DEPRECATED", fmt.Sprintf("%s: %s v%d is deprecated (latest major: v%d)", where, label, major, majors[len(majors)-1])})
				case outdated && majors[len(majors)-1] > major:
					out = append(out, finding{"OUTDATED", fmt.Sprintf("%s: %s v%d has newer major v%d", where, label, major, majors[len(majors)-1])})
				}
			}
		}
	}
	return out
}
