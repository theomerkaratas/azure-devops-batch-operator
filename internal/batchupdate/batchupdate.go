// Package batchupdate runs the shared plan -> confirm -> apply flow used by commands that
// bulk-modify release pipeline definitions.
package batchupdate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/manifest"
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
	raw      map[string]interface{}
	original map[string]interface{}
	id       int
	name     string
	path     string
	lines    []string
}

// Run selects the pipelines, applies mutate to each, prints the plan and (unless dry-run) saves.
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
		original, err := manifest.Clone(raw)
		if err != nil {
			return err
		}
		lines := mutate(cfg, project, raw)
		if len(lines) == 0 {
			unchanged++
			continue
		}
		toUpdate = append(toUpdate, planItem{raw: raw, original: original, id: d.ID, name: d.Name, path: path, lines: lines})
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

	m, err := manifest.New(filepath.Base(os.Args[0]), o.Target, o.Filter, project, comment)
	if err != nil {
		return err
	}
	for _, p := range toUpdate {
		m.Entries = append(m.Entries, manifest.Entry{
			DefinitionID: p.id, Name: p.name, Path: p.path, OriginalRevision: manifest.Revision(p.original),
			Changes: p.lines, Original: p.original, Planned: p.raw, Status: manifest.StatusPending,
		})
	}
	// Saved before the first write so an interrupted run can be resumed or rolled back.
	if err := m.Save(); err != nil {
		return fmt.Errorf("save operation manifest: %w", err)
	}
	fmt.Printf("Manifest: %s\n", m.Path())

	ok, failed := 0, 0
	for i, p := range toUpdate {
		e := &m.Entries[i]
		updated, err := cfg.UpdateDefinitionRaw(project, p.raw, comment)
		if err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", p.path, p.name, err)
			e.Status, e.Error = manifest.StatusFailed, err.Error()
			failed++
		} else {
			fmt.Printf("  ok %s\\%s updated (rev=%v)\n", p.path, p.name, updated["revision"])
			e.Status, e.NewRevision = manifest.StatusSucceeded, manifest.Revision(updated)
			ok++
		}
		if err := m.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update manifest: %v\n", err)
		}
	}
	fmt.Printf("\n=== DONE ===\nSucceeded: %d | Failed: %d | Skipped: %d\n", ok, failed, unchanged)
	fmt.Printf("Undo this operation with: a22r rollback-batch %s\n", m.ID)
	if failed > 0 {
		fmt.Printf("Retry the failed pipelines with: a22r resume-batch %s\n", m.ID)
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

// FlattenDetail turns a release definition's variables, stages, jobs and tasks into a flat
// key -> value map, so two definitions can be diffed generically. Shared by compare-pipelines
// and compare-folders.
func FlattenDetail(cfg azuredevops.Config, project string, detail azuredevops.DefinitionDetail) map[string]string {
	out := map[string]string{}
	addVars := func(prefix string, vars map[string]azuredevops.ConfigVariable) {
		for n, v := range vars {
			if v.IsSecret {
				out[prefix+"variable "+n] = "(secret)"
			} else {
				out[prefix+"variable "+n] = v.Value
			}
		}
	}
	addVars("", detail.Variables)
	for _, env := range detail.Environments {
		sp := "stage " + env.Name + ": "
		out[sp+"exists"] = "yes"
		addVars(sp, env.Variables)
		for _, phase := range env.DeployPhases {
			jp := sp + "job " + phase.Name + ": "
			if di := phase.DeploymentInput; di != nil {
				out[jp+"agent pool"] = cfg.ResolvePoolName(project, di.QueueID)
				out[jp+"demands"] = strings.Join(di.Demands, "; ")
				out[jp+"timeout (min)"] = fmt.Sprint(di.TimeoutInMinutes)
				out[jp+"condition"] = di.Condition
			}
			for i, t := range phase.WorkflowTasks {
				out[fmt.Sprintf("%stask %02d", jp, i+1)] = fmt.Sprintf("%s (enabled=%v)", t.Name, t.Enabled)
			}
		}
	}
	return out
}

// DiffFlat prints the differences between two flattened definitions (as produced by
// FlattenDetail), prefixed "- only in A", "+ only in B" or "~ differs". It returns the number
// of differences found.
func DiffFlat(labelA, labelB string, a, b map[string]string) int {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	diffs := 0
	for _, k := range sorted {
		va, inA := a[k]
		vb, inB := b[k]
		switch {
		case inA && !inB:
			fmt.Printf("- only in %s  %s = %s\n", labelA, k, va)
		case !inA && inB:
			fmt.Printf("+ only in %s  %s = %s\n", labelB, k, vb)
		case va != vb:
			fmt.Printf("~ differs    %s\n    %s: %s\n    %s: %s\n", k, labelA, va, labelB, vb)
		default:
			continue
		}
		diffs++
	}
	return diffs
}
