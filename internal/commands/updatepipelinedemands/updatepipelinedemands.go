// Command update-pipeline-demands bulk-updates the demands of Azure DevOps release pipelines
// under a given path.
package updatepipelinedemands

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  # See the planned changes first (--dry-run):
  update-pipeline-demands 'Example.Project\TEST\CONFIG' --demand 'Agent.Name -equals build-agent-01' --dry-run

  # Apply the changes after confirming:
  update-pipeline-demands 'Example.Project\TEST\CONFIG' --demand 'Agent.Name -equals build-agent-01'

  # Clear the demands list entirely:
  update-pipeline-demands 'Example.Project\TEST\CONFIG' --clear -y

Arguments:
  target       Required. A target path: either a folder (covers all pipelines under it) or the
               full path of a single pipeline.
               E.g.: 'Example.Project\TEST\CONFIG' or 'Example.Project\TEST\CONFIG\TESTAPP\Inspector'

  --demand     A demand rule to add/set. May be given multiple times. Cannot be combined
               meaningfully with --clear.
               E.g.: --demand 'Agent.Name -equals build-agent-01'
               At least one of --demand or --clear is required.

  --clear      Clears the demands list entirely.

  --mode       Optional. How --demand changes the current demands list (default: set).
               Choices: set | add | remove
                 set    -> Replaces the current demands list with the given one.
                 add    -> Adds the given demands to the current list (if missing).
                 remove -> Removes the given demands from the current list.

  --stage      Optional. Only update jobs under the given stage name (e.g. Development).
               If omitted, all stages are processed.

  --level      Optional. The PAT authorization level to use (default: read-write).
               Choices: read-write | manage
                 read-write -> Sufficient to update pipeline definitions (recommended, least privilege).
                 manage     -> For advanced definition/queue management cases.
               Note: this command writes data, so 'read' is not supported.

  --dry-run    Optional. Doesn't update any pipeline; lists the changes that would be made.

  -y, --yes    Optional. Skips the confirmation prompt and applies changes directly.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-demands", flag.ExitOnError)
	demand := multiFlag{}
	fs.Var(&demand, "demand", "A demand rule to add/set. May be given multiple times.")
	clear := fs.Bool("clear", false, "Clears the demands list entirely")
	mode := fs.String("mode", "set", "How --demand updates the current demands: set, add, or remove (default: set)")
	stage := fs.String("stage", "", "Only update jobs under the given stage name")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Doesn't update any pipeline; lists the changes that would be made")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt and applies changes directly")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-updates the demands of release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: update-pipeline-demands <target> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}

	valueFlags := map[string]bool{"demand": true, "mode": true, "stage": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	target := fs.Arg(0)

	if len(demand) == 0 && !*clear {
		fmt.Fprintln(os.Stderr, "Error: you must specify at least one --demand or the --clear flag.")
		os.Exit(2)
	}
	if *mode != "set" && *mode != "add" && *mode != "remove" {
		fmt.Fprintln(os.Stderr, "Error: --mode must be one of: set, add, remove")
		os.Exit(2)
	}
	if *level != "read-write" && *level != "manage" {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}

	opts := options{
		target:      target,
		demands:     []string(demand),
		mode:        *mode,
		clear:       *clear,
		stageFilter: *stage,
		level:       *level,
		dryRun:      *dryRun,
		autoYes:     *yes,
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// multiFlag collects repeated -demand flag occurrences.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type options struct {
	target      string
	demands     []string
	mode        string
	clear       bool
	stageFilter string
	level       string
	dryRun      bool
	autoYes     bool
}

// change describes a single demands update within one pipeline definition.
type change struct {
	stage   string
	job     string
	oldDems []string
	newDems []string
}

// planItem is one matched pipeline and whatever demands changes it needs.
type planItem struct {
	raw       map[string]interface{}
	id        int
	name      string
	path      string
	hasChange bool
	changes   []change
}

func run(o options) error {
	cfg, err := azuredevops.LoadConfig(o.level)
	if err != nil {
		return err
	}

	project, matchedDefs, err := cfg.FindDefinitionsUnderPath(o.target)
	if err != nil {
		return err
	}
	if len(matchedDefs) == 0 {
		fmt.Printf("No matching release pipelines found under '%s'.\n", o.target)
		return nil
	}

	fmt.Printf("Target: %s\n", o.target)
	fmt.Printf("Pipelines found: %d\n", len(matchedDefs))
	fmt.Printf("Mode: %s | Level: %s (least privilege)\n", o.mode, o.level)
	if o.clear {
		fmt.Println("Demands to apply: [CLEAR / EMPTY]")
	} else {
		fmt.Printf("Demands to apply: %v\n", o.demands)
	}
	if o.dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no changes will be saved) <<<")
	}
	fmt.Println()

	total := len(matchedDefs)
	fmt.Printf("Reading pipeline details (0/%d)...", total)
	details, err := fetchDefinitionDetails(cfg, project, matchedDefs, func(completed int) {
		fmt.Printf("\rReading pipeline details (%d/%d)...", completed, total)
	})
	if err != nil {
		return err
	}
	fmt.Println(" Done!")
	fmt.Println()

	plan := buildPlan(matchedDefs, details, o)

	var toUpdate, unchanged []planItem
	for _, p := range plan {
		if p.hasChange {
			toUpdate = append(toUpdate, p)
		} else {
			unchanged = append(unchanged, p)
		}
	}

	fmt.Println("=== PLANNED CHANGES ===")
	for _, p := range toUpdate {
		fmt.Printf("\n[WILL UPDATE] %s\\%s (id=%d)\n", p.path, p.name, p.id)
		for _, ch := range p.changes {
			fmt.Printf("  Stage: '%s' / Job: '%s'\n", ch.stage, ch.job)
			fmt.Printf("    Before: %s\n", demandsOrNone(ch.oldDems))
			fmt.Printf("    After : %s\n", demandsOrNone(ch.newDems))
		}
	}

	if len(unchanged) > 0 {
		fmt.Printf("\n=== ALREADY UP TO DATE / NO CHANGE (%d) ===\n", len(unchanged))
		for i, p := range unchanged {
			if i >= 5 {
				fmt.Printf("  ... and %d more\n", len(unchanged)-5)
				break
			}
			fmt.Printf("  - %s\\%s\n", p.path, p.name)
		}
	}

	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Total matched : %d\n", len(plan))
	fmt.Printf("To update     : %d\n", len(toUpdate))
	fmt.Printf("Unchanged     : %d\n", len(unchanged))
	fmt.Println("------------------------------------------------------------")

	if len(toUpdate) == 0 {
		fmt.Println("No pipelines need updating, they are all already in the desired state.")
		return nil
	}

	if o.dryRun {
		fmt.Println("\nDry-run complete. No changes were made.")
		return nil
	}

	if !o.autoYes {
		fmt.Printf("\n%d pipeline(s) will be updated. Continue? (y/N): ", len(toUpdate))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Println("\nApplying updates...")
	successCount, failCount := 0, 0
	for _, p := range toUpdate {
		comment := fmt.Sprintf("Automated demands update: %v", o.demands)
		if o.clear {
			comment = "Automated demands update: cleared"
		}
		updated, err := cfg.UpdateDefinitionRaw(project, p.raw, comment)
		if err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", p.path, p.name, err)
			failCount++
			continue
		}
		fmt.Printf("  ok %s\\%s updated (rev=%v)\n", p.path, p.name, updated["revision"])
		successCount++
	}

	fmt.Println("\n=== DONE ===")
	fmt.Printf("Succeeded: %d | Failed: %d | Skipped: %d\n", successCount, failCount, len(unchanged))
	return nil
}

func demandsOrNone(demands []string) string {
	if len(demands) == 0 {
		return "None"
	}
	return fmt.Sprintf("%v", demands)
}

// fetchDefinitionDetails fetches each definition's raw detail, with up to 10 requests in flight,
// mirroring the Python version's ThreadPoolExecutor(max_workers=10).
func fetchDefinitionDetails(
	cfg azuredevops.Config, project string, defs []azuredevops.ReleaseDefinition, onProgress func(completed int),
) (map[int]map[string]interface{}, error) {
	const maxWorkers = 10

	results := make(map[int]map[string]interface{}, len(defs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)
	errCh := make(chan error, len(defs))
	completed := 0

	for _, d := range defs {
		wg.Add(1)
		sem <- struct{}{}
		go func(d azuredevops.ReleaseDefinition) {
			defer wg.Done()
			defer func() { <-sem }()

			detail, err := cfg.GetDefinitionDetailRaw(project, d.ID)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			results[d.ID] = detail
			completed++
			onProgress(completed)
			mu.Unlock()
		}(d)
	}
	wg.Wait()
	close(errCh)

	if err, ok := <-errCh; ok {
		return nil, err
	}
	return results, nil
}

// computeNewDemands applies --mode/--clear to a current demands list.
func computeNewDemands(current, target []string, mode string, clear bool) []string {
	if clear {
		return []string{}
	}
	switch mode {
	case "set":
		return append([]string(nil), target...)
	case "add":
		newList := append([]string(nil), current...)
		for _, d := range target {
			if !contains(newList, d) {
				newList = append(newList, d)
			}
		}
		return newList
	case "remove":
		var newList []string
		for _, d := range current {
			if !contains(target, d) {
				newList = append(newList, d)
			}
		}
		return newList
	default:
		return current
	}
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// buildPlan walks each definition's raw JSON, computing demand changes and mutating the raw map
// in place so it can be PUT back unchanged apart from the demands field.
func buildPlan(defs []azuredevops.ReleaseDefinition, details map[int]map[string]interface{}, o options) []planItem {
	plan := make([]planItem, 0, len(defs))

	for _, d := range defs {
		raw := details[d.ID]
		name, _ := raw["name"].(string)
		path, _ := raw["path"].(string)
		if path == "" {
			path = `\`
		}

		item := planItem{raw: raw, id: d.ID, name: name, path: path}

		environments, _ := raw["environments"].([]interface{})
		for _, envRaw := range environments {
			env, ok := envRaw.(map[string]interface{})
			if !ok {
				continue
			}
			envName, _ := env["name"].(string)
			if o.stageFilter != "" && !strings.EqualFold(envName, o.stageFilter) {
				continue
			}

			phases, _ := env["deployPhases"].([]interface{})
			for _, phaseRaw := range phases {
				phase, ok := phaseRaw.(map[string]interface{})
				if !ok {
					continue
				}
				di, ok := phase["deploymentInput"].(map[string]interface{})
				if !ok || di == nil {
					continue
				}
				jobName, _ := phase["name"].(string)

				current := toStringSlice(di["demands"])
				updated := computeNewDemands(current, o.demands, o.mode, o.clear)

				if !equalStrings(current, updated) {
					item.hasChange = true
					item.changes = append(item.changes, change{
						stage: envName, job: jobName, oldDems: current, newDems: updated,
					})
					di["demands"] = toInterfaceSlice(updated)
				}
			}
		}

		plan = append(plan, item)
	}

	sort.Slice(plan, func(i, j int) bool { return plan[i].path+plan[i].name < plan[j].path+plan[j].name })
	return plan
}

func toStringSlice(v interface{}) []string {
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toInterfaceSlice(v []string) []interface{} {
	out := make([]interface{}, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
