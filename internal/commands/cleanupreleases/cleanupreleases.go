// Command cleanup-releases deletes old release instances by age, status, and retention counts.
// It only lists what it would delete unless --apply is given.
package cleanupreleases

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  cleanup-releases 'Example.Project\TEST' --older-than 90 --keep-successful 5
  cleanup-releases 'Example.Project\TEST' --status abandoned,failed --older-than 30 --apply
Rules (a release is a candidate only if it matches every rule given; at least one of --older-than
or --status is required):
  --older-than DAYS      Created more than DAYS days ago.
  --status LIST          Comma-separated: succeeded, failed, canceled, abandoned, draft, notdeployed.
Protection (always applied, never deleted):
  - releases marked "retain indefinitely" (keep forever)
  - releases with a deployment in progress or queued
  - the newest --keep-latest releases of each pipeline (default 3)
  - the newest --keep-successful succeeded releases of each pipeline (default 0)
Safety: nothing is deleted unless --apply is given; without it the command is a dry run. With --apply
a confirmation prompt is shown unless -y is also given. Deleted releases cannot be restored with this
tool; Azure DevOps keeps them for the project's "permanently destroy releases" period before they are
destroyed for good. Releases still held by retention leases are refused and reported as errors.
Statuses: succeeded = every started stage succeeded; failed = a stage was rejected or partially succeeded;
canceled = a stage was canceled and none failed; notdeployed = nothing has been deployed yet.
`

// maxOlderThanDays bounds --older-than (100 years) so the age cutoff cannot overflow.
const maxOlderThanDays = 36500

var validStatus = map[string]bool{"succeeded": true, "failed": true, "canceled": true, "abandoned": true, "draft": true, "notdeployed": true}

// rules are the selection settings.
type rules struct {
	olderThanDays  int
	statuses       map[string]bool
	keepLatest     int
	keepSuccessful int
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("cleanup-releases", flag.ExitOnError)
	var r rules
	fs.IntVar(&r.olderThanDays, "older-than", 0, "Only releases created more than this many days ago")
	statusList := fs.String("status", "", "Comma-separated release statuses to delete")
	fs.IntVar(&r.keepLatest, "keep-latest", 3, "Always keep this many newest releases per pipeline")
	fs.IntVar(&r.keepSuccessful, "keep-successful", 0, "Always keep this many newest succeeded releases per pipeline")
	maxScan := fs.Int("max-scan", 1000, "Maximum releases to read per pipeline")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	level := fs.String("level", "manage", "PAT authorization level to use (must be manage)")
	apply := fs.Bool("apply", false, "Actually delete releases (default: dry run)")
	dryRun := fs.Bool("dry-run", false, "Explicit dry run (this is the default)")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt (only with --apply)")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Deletes old release instances. Dry run unless --apply is given.")
		fmt.Fprintln(os.Stderr, "\nUsage: cleanup-releases <target> [--older-than DAYS] [--status LIST] [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"older-than": true, "status": true, "keep-latest": true, "keep-successful": true, "max-scan": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	r.statuses = map[string]bool{}
	for _, s := range strings.Split(*statusList, ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			if !validStatus[s] {
				fmt.Fprintf(os.Stderr, "Error: unknown status %q\n", s)
				os.Exit(2)
			}
			r.statuses[s] = true
		}
	}
	switch {
	case fs.NArg() != 1:
		fs.Usage()
		os.Exit(2)
	case r.olderThanDays <= 0 && len(r.statuses) == 0:
		fmt.Fprintln(os.Stderr, "Error: specify --older-than and/or --status")
		os.Exit(2)
	case r.olderThanDays > maxOlderThanDays:
		fmt.Fprintf(os.Stderr, "Error: --older-than must be at most %d days\n", maxOlderThanDays)
		os.Exit(2)
	case r.olderThanDays < 0 || r.keepLatest < 0 || r.keepSuccessful < 0 || *maxScan < 1:
		fmt.Fprintln(os.Stderr, "Error: numeric options must not be negative (--max-scan at least 1)")
		os.Exit(2)
	case *apply && *dryRun:
		fmt.Fprintln(os.Stderr, "Error: --apply and --dry-run cannot be combined")
		os.Exit(2)
	case *yes && !*apply:
		fmt.Fprintln(os.Stderr, "Error: --yes only makes sense with --apply")
		os.Exit(2)
	case *level != "manage":
		fmt.Fprintln(os.Stderr, "Error: --level must be manage for deleting releases")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *level, r, *maxScan, *apply, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

type candidate struct {
	release azuredevops.Release
	state   string
}

// classify summarizes a release as succeeded, failed, canceled, abandoned, draft, notdeployed or inprogress.
func classify(r azuredevops.Release) string {
	switch strings.ToLower(r.Status) {
	case "abandoned":
		return "abandoned"
	case "draft":
		return "draft"
	}
	succeeded, failed, canceled, started := false, false, false, false
	for _, e := range r.Environments {
		switch strings.ToLower(e.Status) {
		case "inprogress", "queued", "scheduled":
			return "inprogress"
		case "succeeded":
			succeeded, started = true, true
		case "rejected", "partiallysucceeded":
			failed, started = true, true
		case "canceled":
			canceled, started = true, true
		}
	}
	switch {
	case failed:
		return "failed"
	case canceled:
		return "canceled"
	case succeeded && started:
		return "succeeded"
	}
	return "notdeployed"
}

// selectForDeletion picks deletable releases from a pipeline's releases (newest first).
func selectForDeletion(releases []azuredevops.Release, r rules, now time.Time) []candidate {
	var out []candidate
	successSeen := 0
	for i, rel := range releases {
		state := classify(rel)
		if state == "succeeded" {
			successSeen++
		}
		switch {
		case rel.KeepForever, state == "inprogress", i < r.keepLatest:
			continue
		case state == "succeeded" && successSeen <= r.keepSuccessful:
			continue
		case len(r.statuses) > 0 && !r.statuses[state]:
			continue
		}
		if r.olderThanDays > 0 {
			created, err := time.Parse(time.RFC3339, rel.CreatedOn)
			if err != nil || created.After(now.AddDate(0, 0, -r.olderThanDays)) {
				continue
			}
		}
		out = append(out, candidate{rel, state})
	}
	return out
}

func listAll(cfg azuredevops.Config, project string, defID, maxScan int) ([]azuredevops.Release, error) {
	var all []azuredevops.Release
	seen := map[int]bool{}
	continuation := 0
	for len(all) < maxScan {
		page, err := cfg.ListReleasesPage(project, defID, 100, continuation)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		added := 0
		for _, rel := range page {
			if !seen[rel.ID] {
				seen[rel.ID] = true
				all = append(all, rel)
				added++
			}
		}
		if added == 0 {
			return nil, fmt.Errorf("release paging returned only already-seen releases; refusing to continue with an incomplete list")
		}
		if len(page) < 100 {
			break
		}
		continuation = page[len(page)-1].ID
	}
	if len(all) > maxScan {
		all = all[:maxScan]
	}
	return all, nil
}

func run(target, filter, level string, r rules, maxScan int, apply, autoYes bool) error {
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
	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))
	if !apply {
		fmt.Println(">>> DRY RUN: nothing will be deleted (use --apply to delete) <<<")
	}
	type plan struct {
		name       string
		candidates []candidate
	}
	var plans []plan
	total := 0
	for _, d := range defs {
		releases, err := listAll(cfg, project, d.ID, maxScan)
		if err != nil {
			return fmt.Errorf("%s: %w", d.Name, err)
		}
		picked := selectForDeletion(releases, r, time.Now())
		if len(picked) > 0 {
			plans = append(plans, plan{d.Name, picked})
			total += len(picked)
		}
	}
	fmt.Println("\n=== RELEASES TO DELETE ===")
	for _, p := range plans {
		fmt.Printf("\n%s (%d)\n", p.name, len(p.candidates))
		for _, c := range p.candidates {
			fmt.Printf("  %-20s id=%-8d %-12s created %s\n", c.release.Name, c.release.ID, c.state, c.release.CreatedOn)
		}
	}
	fmt.Printf("\n%d release(s) in %d pipeline(s) match.\n", total, len(plans))
	if total == 0 {
		return nil
	}
	if !apply {
		fmt.Println("\nDry run complete. Nothing was deleted. Re-run with --apply to delete these releases.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nPERMANENTLY delete %d release(s)? (y/N): ", total)) {
		fmt.Println("Cancelled.")
		return nil
	}
	ok, failed := 0, 0
	for _, p := range plans {
		for _, c := range p.candidates {
			if err := cfg.DeleteRelease(project, c.release.ID); err != nil {
				fmt.Printf("  x %s / %s ERROR: %v\n", p.name, c.release.Name, err)
				failed++
				continue
			}
			fmt.Printf("  ok %s / %s deleted\n", p.name, c.release.Name)
			ok++
		}
	}
	fmt.Printf("\n=== DONE ===\nDeleted: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d release(s) could not be deleted", failed)
	}
	return nil
}
