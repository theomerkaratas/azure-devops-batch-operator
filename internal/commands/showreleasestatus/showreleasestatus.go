// Command show-release-status lists the succeeded/failed status of the latest run of each
// Azure DevOps release pipeline under a given path.
package showreleasestatus

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  show-release-status 'Example.Project\TEST' --level read
  show-release-status 'Example.Project\TEST\CONFIG\TESTAPP\Inspector' --level read

Arguments:
  target   Required. A target path: either a folder (covers all pipelines under it) or the
           full path of a single pipeline.
           E.g.: 'Example.Project\TEST' or 'Example.Project\TEST\CONFIG\TESTAPP\Inspector'

  --level  Optional. The PAT authorization level to use (default: read).
           Choices: read | read-write | manage
             read        -> Read-only viewing (sufficient and recommended for this command).
             read-write  -> For commands that update pipeline definitions.
             manage      -> For advanced definition/queue management operations.
           This command only reads data, so 'read' is always sufficient.

For each pipeline, computes an overall status (SUCCEEDED, FAILED, PARTIALLY SUCCEEDED, IN
PROGRESS, NO RUNS YET) from the stages of its most recent release, and lists them grouped.
`

// statusLabels maps Azure DevOps environment status values to a display label.
var statusLabels = map[string]string{
	"succeeded":          "SUCCEEDED",
	"partiallySucceeded": "PARTIALLY SUCCEEDED",
	"failed":             "FAILED",
	"canceled":           "CANCELED",
	"rejected":           "REJECTED",
	"inProgress":         "IN PROGRESS",
	"queued":             "QUEUED",
	"scheduled":          "SCHEDULED",
	"notStarted":         "NOT STARTED",
}

var failedStatuses = map[string]bool{"failed": true, "rejected": true}
var runningStatuses = map[string]bool{"inProgress": true, "queued": true, "scheduled": true, "notStarted": true}

func statusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}

// summarizeRelease inspects a release's stage statuses and produces an overall status and
// per-stage detail lines.
func summarizeRelease(release azuredevops.Release) (overall string, details []string) {
	statuses := map[string]bool{}
	for _, env := range release.Environments {
		status := env.Status
		if status == "" {
			status = "unknown"
		}
		statuses[status] = true
		details = append(details, fmt.Sprintf("%s: %s", env.Name, statusLabel(status)))
	}

	switch {
	case len(statuses) == 0:
		overall = "UNKNOWN"
	case hasAny(statuses, failedStatuses):
		overall = "FAILED"
	case hasAny(statuses, runningStatuses):
		overall = "IN PROGRESS"
	case isSubsetOf(statuses, map[string]bool{"succeeded": true, "partiallySucceeded": true}):
		if len(statuses) == 1 && statuses["succeeded"] {
			overall = "SUCCEEDED"
		} else {
			overall = "PARTIALLY SUCCEEDED"
		}
	default:
		var labels []string
		for s := range statuses {
			labels = append(labels, statusLabel(s))
		}
		sortStrings(labels)
		overall = "MIXED (" + strings.Join(labels, ", ") + ")"
	}
	return overall, details
}

func hasAny(set, subset map[string]bool) bool {
	for s := range subset {
		if set[s] {
			return true
		}
	}
	return false
}

func isSubsetOf(set, superset map[string]bool) bool {
	for s := range set {
		if !superset[s] {
			return false
		}
	}
	return true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

type statusRow struct {
	path        string
	overall     string
	details     []string
	releaseName string
	createdOn   string
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("show-release-status", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists the succeeded/failed status of the latest run of each release pipeline under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: show-release-status <target> [--level read|read-write|manage]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"level": true})); err != nil {
		os.Exit(2)
	}

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	target := fs.Arg(0)

	if err := run(target, *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}

	project, matchedDefs, err := cfg.FindDefinitionsUnderPath(target)
	if err != nil {
		return err
	}
	if len(matchedDefs) == 0 {
		fmt.Printf("No matching release pipelines found under '%s'.\n", target)
		return nil
	}

	fmt.Printf("Target: %s\n", target)
	fmt.Printf("Pipelines found: %d\n", len(matchedDefs))
	fmt.Println("Querying latest runs...")
	fmt.Println()

	releases, err := fetchLatestReleases(cfg, project, matchedDefs)
	if err != nil {
		return err
	}

	rows := make([]statusRow, 0, len(matchedDefs))
	for _, d := range matchedDefs {
		folder := d.Path
		if folder == "" {
			folder = `\`
		}
		fullPath := folder + `\` + d.Name

		release := releases[d.ID]
		if release == nil {
			rows = append(rows, statusRow{path: fullPath, overall: "NO RUNS YET"})
			continue
		}
		overall, details := summarizeRelease(*release)
		rows = append(rows, statusRow{
			path: fullPath, overall: overall, details: details,
			releaseName: release.Name, createdOn: release.CreatedOn,
		})
	}

	var succeeded, failed, others []statusRow
	for _, r := range rows {
		switch r.overall {
		case "SUCCEEDED":
			succeeded = append(succeeded, r)
		case "FAILED":
			failed = append(failed, r)
		default:
			others = append(others, r)
		}
	}

	fmt.Println("=== FAILED ===")
	if len(failed) == 0 {
		fmt.Println("  (none)")
	}
	for _, r := range failed {
		fmt.Printf("  [X] %s  (release: %s, date: %s)\n", r.path, r.releaseName, r.createdOn)
		for _, d := range r.details {
			fmt.Printf("        - %s\n", d)
		}
	}

	fmt.Println()
	fmt.Println("=== SUCCEEDED ===")
	if len(succeeded) == 0 {
		fmt.Println("  (none)")
	}
	for _, r := range succeeded {
		fmt.Printf("  [OK] %s  (release: %s, date: %s)\n", r.path, r.releaseName, r.createdOn)
	}

	if len(others) > 0 {
		fmt.Println()
		fmt.Println("=== OTHER (in progress / unknown / no runs) ===")
		for _, r := range others {
			fmt.Printf("  [?] %s  -> %s\n", r.path, r.overall)
		}
	}

	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Total: %d | Succeeded: %d | Failed: %d | Other: %d\n", len(rows), len(succeeded), len(failed), len(others))
	fmt.Println("------------------------------------------------------------")
	return nil
}

// fetchLatestReleases fetches the latest release for each definition, with up to 10 requests
// in flight at once, mirroring the Python version's ThreadPoolExecutor(max_workers=10).
func fetchLatestReleases(cfg azuredevops.Config, project string, defs []azuredevops.ReleaseDefinition) (map[int]*azuredevops.Release, error) {
	const maxWorkers = 10

	results := make(map[int]*azuredevops.Release, len(defs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)
	errCh := make(chan error, len(defs))

	for _, d := range defs {
		wg.Add(1)
		sem <- struct{}{}
		go func(d azuredevops.ReleaseDefinition) {
			defer wg.Done()
			defer func() { <-sem }()

			release, err := cfg.GetLatestRelease(project, d.ID)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			results[d.ID] = release
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
