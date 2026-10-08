// Command resume-batch finishes a batch operation that was interrupted or partly failed, using
// its saved manifest. Pipelines that were already updated are not touched again.
package resumebatch

import (
	"flag"
	"fmt"
	"os"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
	"github.com/omerkaratas/azure-devops-go-automations/internal/manifest"
)

const usageEpilog = `
Examples:
  resume-batch 20260101-120000-ab12cd --dry-run
  resume-batch 20260101-120000-ab12cd -y
Arguments:
  manifest   Required. Manifest ID (or unique prefix) printed by the original operation; see
             list-batch-manifests.
  --level    Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run  Lists what would be resumed without changing anything.
  -y, --yes  Skips the confirmation prompt.
Only pipelines still pending or failed are applied, and only if their revision is unchanged since
the original operation read them. A pipeline modified by someone else in the meantime is reported
and left alone.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("resume-batch", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists what would be resumed without changing anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Resumes an interrupted or partly failed batch operation from its manifest.")
		fmt.Fprintln(os.Stderr, "\nUsage: resume-batch <manifest-id> [flags]")
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
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

type decision int

const (
	apply decision = iota
	alreadyDone
	conflict
)

// decide compares the live definition with the manifest entry. A revision that is exactly one
// ahead with our comment is treated as our own write whose result was never recorded.
func decide(m *manifest.Manifest, e manifest.Entry, current map[string]interface{}) (decision, string) {
	rev := manifest.Revision(current)
	switch {
	case rev == e.OriginalRevision:
		return apply, ""
	case m.Comment != "" && rev == e.OriginalRevision+1 && current["comment"] == m.Comment:
		return alreadyDone, fmt.Sprintf("already applied (rev=%d)", rev)
	default:
		return conflict, fmt.Sprintf("revision is %d but the operation read %d; changed since", rev, e.OriginalRevision)
	}
}

func run(id, level string, dryRun, autoYes bool) error {
	m, err := manifest.Load(id)
	if err != nil {
		return err
	}
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	fmt.Printf("Manifest: %s (%s on %s, %s)\nLevel: %s\n", m.ID, m.Command, m.Target, m.Created.Local().Format("2006-01-02 15:04"), level)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no changes will be saved) <<<")
	}

	var todo []int
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.Status != manifest.StatusPending && e.Status != manifest.StatusFailed {
			continue
		}
		current, err := cfg.GetDefinitionDetailRaw(m.Project, e.DefinitionID)
		if err != nil {
			fmt.Printf("  x %s\\%s cannot read current definition: %v\n", e.Path, e.Name, err)
			continue
		}
		switch d, why := decide(m, *e, current); d {
		case apply:
			fmt.Printf("[WILL RESUME] %s\\%s (id=%d)\n", e.Path, e.Name, e.DefinitionID)
			todo = append(todo, i)
		case alreadyDone:
			fmt.Printf("  = %s\\%s %s\n", e.Path, e.Name, why)
			e.Status, e.NewRevision, e.Error = manifest.StatusSucceeded, manifest.Revision(current), ""
		case conflict:
			fmt.Printf("  ! %s\\%s skipped: %s\n", e.Path, e.Name, why)
		}
	}
	counts := m.Counts()
	fmt.Printf("\nAlready succeeded: %d | To resume: %d\n", counts[manifest.StatusSucceeded], len(todo))
	if dryRun {
		fmt.Println("\nDry-run complete. No changes were made.")
		return nil
	}
	if len(todo) == 0 {
		_ = m.Save()
		fmt.Println("Nothing to resume.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\n%d pipeline(s) will be updated. Continue? (y/N): ", len(todo))) {
		fmt.Println("Cancelled.")
		return nil
	}

	failed := 0
	for _, i := range todo {
		e := &m.Entries[i]
		// Fresh revision check happened above; the server also rejects a stale revision.
		updated, err := cfg.UpdateDefinitionRaw(m.Project, e.Planned, m.Comment)
		if err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", e.Path, e.Name, err)
			e.Status, e.Error = manifest.StatusFailed, err.Error()
			failed++
		} else {
			fmt.Printf("  ok %s\\%s updated (rev=%v)\n", e.Path, e.Name, updated["revision"])
			e.Status, e.NewRevision, e.Error = manifest.StatusSucceeded, manifest.Revision(updated), ""
		}
		if err := m.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update manifest: %v\n", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d pipeline update(s) failed", failed)
	}
	return nil
}
