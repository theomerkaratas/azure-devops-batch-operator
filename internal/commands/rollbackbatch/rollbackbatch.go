// Command rollback-batch restores the definitions captured before an earlier batch operation,
// using its saved manifest.
package rollbackbatch

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
  rollback-batch 20260101-120000-ab12cd --dry-run
  rollback-batch 20260101-120000-ab12cd -y
Arguments:
  manifest   Required. Manifest ID (or unique prefix) printed by the original operation; see
             list-batch-manifests.
  --level    Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run  Lists what would be rolled back without changing anything.
  -y, --yes  Skips the confirmation prompt.
Only pipelines the operation updated are rolled back, and only if their current revision is still
the one the operation produced. A pipeline edited by anyone since is reported and left alone, so
legitimate later changes are never overwritten. Secret variable values are masked by Azure
DevOps and cannot be recovered by a rollback.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("rollback-batch", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists what would be rolled back without changing anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Rolls back a batch operation to the definitions captured before it ran.")
		fmt.Fprintln(os.Stderr, "\nUsage: rollback-batch <manifest-id> [flags]")
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

	type item struct {
		idx     int
		current map[string]interface{}
	}
	var todo []item
	conflicts := 0
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.Status != manifest.StatusSucceeded {
			continue
		}
		current, err := cfg.GetDefinitionDetailRaw(m.Project, e.DefinitionID)
		if err != nil {
			fmt.Printf("  x %s\\%s cannot read current definition: %v\n", e.Path, e.Name, err)
			conflicts++
			continue
		}
		if rev := manifest.Revision(current); rev != e.NewRevision {
			fmt.Printf("  ! %s\\%s skipped: revision is %d but the operation produced %d; modified since\n", e.Path, e.Name, rev, e.NewRevision)
			conflicts++
			continue
		}
		fmt.Printf("[WILL ROLL BACK] %s\\%s (id=%d, rev %d -> original rev %d)\n", e.Path, e.Name, e.DefinitionID, e.NewRevision, e.OriginalRevision)
		todo = append(todo, item{i, current})
	}
	fmt.Printf("\nTo roll back: %d | Skipped (changed since): %d\n", len(todo), conflicts)
	if dryRun {
		fmt.Println("\nDry-run complete. No changes were made.")
		return nil
	}
	if len(todo) == 0 {
		fmt.Println("Nothing to roll back.")
		if conflicts > 0 {
			return fmt.Errorf("%d pipeline(s) could not be rolled back", conflicts)
		}
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\n%d pipeline(s) will be rolled back. Continue? (y/N): ", len(todo))) {
		fmt.Println("Cancelled.")
		return nil
	}

	failed := 0
	for _, it := range todo {
		e := &m.Entries[it.idx]
		raw, err := manifest.Clone(e.Original)
		if err != nil {
			return err
		}
		raw["revision"] = it.current["revision"]
		updated, err := cfg.UpdateDefinitionRaw(m.Project, raw, "Rolled back batch operation "+m.ID)
		if err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", e.Path, e.Name, err)
			e.Error = "rollback: " + err.Error()
			failed++
		} else {
			fmt.Printf("  ok %s\\%s rolled back (rev=%v)\n", e.Path, e.Name, updated["revision"])
			e.Status, e.RolledBackRev, e.Error = manifest.StatusRolledBack, manifest.Revision(updated), ""
		}
		if err := m.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update manifest: %v\n", err)
		}
	}
	if failed > 0 || conflicts > 0 {
		return fmt.Errorf("%d rollback(s) failed, %d skipped", failed, conflicts)
	}
	return nil
}
