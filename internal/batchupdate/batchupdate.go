// Package batchupdate runs the shared plan -> confirm -> apply flow used by commands that
// bulk-modify release pipeline definitions.
package batchupdate

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

// Options are the settings common to all batch commands.
type Options struct {
	Target  string
	Filter  string // case-insensitive substring of the pipeline name; empty matches all
	Level   string
	DryRun  bool
	AutoYes bool

	// BeforeApply runs once after confirmation and before any definition is saved.
	BeforeApply func(cfg azuredevops.Config, project string) error
}

// Mutator edits a raw definition in place and returns one line per change made (none if unchanged).
type Mutator func(cfg azuredevops.Config, project string, raw map[string]interface{}) []string

// ValidWriteLevel reports whether level is allowed for commands that write data.
func ValidWriteLevel(level string) bool { return level == "read-write" || level == "manage" }

// SelectDefinitions returns the project and the definitions matching target and the name filter.
func SelectDefinitions(cfg azuredevops.Config, target, filter string) (string, []azuredevops.ReleaseDefinition, error) {
	project, defs, err := cfg.FindDefinitionsUnderPath(target)
	if err != nil {
		return "", nil, err
	}
	if filter == "" {
		return project, defs, nil
	}
	var out []azuredevops.ReleaseDefinition
	for _, d := range defs {
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(filter)) {
			out = append(out, d)
		}
	}
	return project, out, nil
}

// Confirm asks a y/N question on stdin.
func Confirm(prompt string) bool {
	fmt.Print(prompt)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

type planItem struct {
	raw   map[string]interface{}
	id    int
	name  string
	path  string
	lines []string
}

// Run selects the pipelines, applies mutate to each, shows the plan and (unless dry-run) saves.
func Run(o Options, comment string, mutate Mutator) error {
	cfg, err := azuredevops.LoadConfig(o.Level)
	if err != nil {
		return err
	}
	project, defs, err := SelectDefinitions(cfg, o.Target, o.Filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under `%s`.\n", o.Target)
		return nil
	}

	fmt.Printf("Target: %s\nPipelines found: %d\nLevel: %s\n", o.Target, len(defs), o.Level)
	if o.DryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no changes will be saved) <<<")
	}
	fmt.Printf("\nReading %d pipeline definition(s)...\n", len(defs))
	details, err := fetchDetails(cfg, project, defs)
	if err != nil {
		return err
	}

	var toUpdate []planItem
	unchanged := 0
	for _, d := range defs {
		raw := details[d.ID]
		path := d.Path
		if path == "" {
			path = `\`
		}
		lines := mutate(cfg, project, raw)
		if len(lines) == 0 {
			unchanged++
			continue
		}
		toUpdate = append(toUpdate, planItem{raw: raw, id: d.ID, name: d.Name, path: path, lines: lines})
	}

	fmt.Println("\n=== PLANNED CHANGES ===")
	for _, p := range toUpdate {
		fmt.Printf("\n[WILL UPDATE] %s\\%s (id=%d)\n", p.path, p.name, p.id)
		for _, l := range p.lines {
			fmt.Printf("  %s\n", l)
		}
	}

	fmt.Println("\n============================================================")
	fmt.Printf("Total matched : %d\nTo update     : %d\nUnchanged     : %d\n", len(defs), len(toUpdate), unchanged)
	fmt.Println("============================================================")

	if len(toUpdate) == 0 {
		fmt.Println("Nothing to do, all pipelines are already in the desired state.")
		return nil
	}

	if o.DryRun {
		fmt.Println("\nDry-run complete. No changes were made.")
		return nil
	}

	if !o.AutoYes && !Confirm(fmt.Sprintf("\n%d pipeline(s) will be updated. Continue? (y/N): ", len(toUpdate))) {
		fmt.Println("Cancelled.")
		return nil
	}

	fmt.Println("\nApplying updates...")
	if o.BeforeApply != nil {
		if err := o.BeforeApply(cfg, project); err != nil {
			return err
		}
	}

	ok, failed := 0, 0
	for _, p := range toUpdate {
		updated, err := cfg.UpdateDefinitionRaw(project, p.raw, comment)
		if err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", p.path, p.name, err)
			failed++
			continue
		}
		fmt.Printf("  ok %s\\%s updated (rev=%v)\n", p.path, p.name, updated["revision"])
		ok++
	}
	fmt.Printf("\n=== DONE ===\nSucceeded: %d | Failed: %d | Skipped: %d\n", ok, failed, unchanged)
	if failed > 0 {
		return fmt.Errorf("%d pipeline update(s) failed", failed)
	}
	return nil
}

// fetchDetails fetches raw definitions with up to 10 requests in flight.
func fetchDetails(cfg azuredevops.Config, project string, defs []azuredevops.ReleaseDefinition) (map[int]map[string]interface{}, error) {
	results := make(map[int]map[string]interface{}, len(defs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	errCh := make(chan error, len(defs))

	for _, d := range defs {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			detail, err := cfg.GetDefinitionDetailRaw(project, id)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			results[id] = detail
			mu.Unlock()
		}(d.ID)
	}
	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok {
		return nil, err
	}
	return results, nil
}

// EachEnvironment calls fn for every stage of raw whose name matches stage (all if stage is empty).
func EachEnvironment(raw map[string]interface{}, stage string, fn func(name string, env map[string]interface{})) {
	envs, _ := raw["environments"].([]interface{})
	for _, e := range envs {
		env, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := env["name"].(string)
		if stage != "" && !strings.EqualFold(name, stage) {
			continue
		}
		fn(name, env)
	}
}

// EachDeploymentInput calls fn for every agent job's deploymentInput in the matching stages.
func EachDeploymentInput(raw map[string]interface{}, stage string, fn func(stage, job string, di map[string]interface{})) {
	EachEnvironment(raw, stage, func(envName string, env map[string]interface{}) {
		phases, _ := env["deployPhases"].([]interface{})
		for _, p := range phases {
			phase, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			di, ok := phase["deploymentInput"].(map[string]interface{})
			if !ok {
				continue
			}
			job, _ := phase["name"].(string)
			fn(envName, job, di)
		}
	})
}

// Int reads a JSON number (decoded as float64) from m[key].
func Int(m map[string]interface{}, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}
