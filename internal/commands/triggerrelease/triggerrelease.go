// Command trigger-release creates new releases for many Azure DevOps release pipelines at once.
package triggerrelease

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  # Preview which pipelines would get a release:
  trigger-release 'Example.Project\TEST\CONFIG' --dry-run
  # Release every pipeline under a folder whose name contains 'WCF':
  trigger-release 'Example.Project\TEST\CONFIG' --filter WCF --description 'Config refresh'
  # Start only the Development stage manually on one pipeline:
  trigger-release 'Example.Project\TEST\CONFIG\TESTAPP\Inspector' --stage Development -y
Arguments:
  target         Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --filter       Optional. Only pipelines whose name contains this text (case-insensitive).
  --description  Optional. Description stored on each created release.
  --stage        Optional. Stage to start manually (stages that don't auto-start). May be given multiple times.
                 Artifacts use their latest versions.
  --level        Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run      Lists the pipelines that would be released without creating anything.
  -y, --yes      Skips the confirmation prompt.
`

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("trigger-release", flag.ExitOnError)
	stages := multiFlag{}
	fs.Var(&stages, "stage", "Stage to start manually. May be given multiple times.")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	description := fs.String("description", "Triggered by azure-devops-batch-operator", "Description stored on each created release")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists the pipelines that would be released without creating anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Creates new releases for all release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: trigger-release <target> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"stage": true, "filter": true, "description": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
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
	if err := run(fs.Arg(0), *filter, *description, []string(stages), *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(target, filter, description string, stages []string, level string, dryRun, autoYes bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under '%s'.\n", target)
		return nil
	}
	fmt.Printf("Target: %s\nPipelines found: %d\nLevel: %s\n", target, len(defs), level)
	if len(stages) > 0 {
		fmt.Printf("Manual stages: %s\n", strings.Join(stages, ", "))
	}
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no releases will be created) <<<")
	}
	fmt.Println("\n=== RELEASES TO CREATE ===")
	for _, d := range defs {
		path := d.Path
		if path == "" {
			path = `\`
		}
		fmt.Printf("  %s\\%s (id=%d)\n", path, d.Name, d.ID)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. No releases were created.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\n%d release(s) will be created. Continue? (y/N): ", len(defs))) {
		fmt.Println("Cancelled.")
		return nil
	}
	fmt.Println("\nCreating releases...")
	ok, failed := 0, 0
	for _, d := range defs {
		rel, err := cfg.CreateRelease(project, d.ID, description, stages)
		if err != nil {
			fmt.Printf("  x %s ERROR: %v\n", d.Name, err)
			failed++
			continue
		}
		fmt.Printf("  ok %s -> %s (id=%d)\n", d.Name, rel.Name, rel.ID)
		ok++
	}
	fmt.Printf("\n=== DONE ===\nCreated: %d | Failed: %d\n", ok, failed)
	return nil
}
