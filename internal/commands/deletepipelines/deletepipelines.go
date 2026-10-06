// Command delete-pipelines permanently deletes Azure DevOps classic release pipeline definitions.
package deletepipelines

import (
	"bufio"
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
  delete-pipelines 'Example.Project\TEST\ObsoletePipeline' --dry-run
  delete-pipelines 'Example.Project\TEST\ARCHIVE' --filter deprecated --yes
Arguments:
  target         Required. A folder (covers all pipelines under it) or one full pipeline path.
  --filter       Optional. Only pipeline names containing this text (case-insensitive).
  --comment      Optional. Audit comment sent to Azure DevOps for each deletion.
  --force        Cancels active deployments when Azure DevOps requires it for deletion.
  --level        Optional. Must be manage (default: manage).
  --dry-run      Lists matching pipelines without deleting them.
  -y, --yes      Skips the confirmation prompt.
Warning: deletion is permanent. Use --dry-run before deleting a folder of pipelines.
`

func Main() {
	fs := flag.NewFlagSet("delete-pipelines", flag.ExitOnError)
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	comment := fs.String("comment", "Deleted by azure-devops-batch-operator", "Audit comment for the deletion")
	force := fs.Bool("force", false, "Cancel active deployments and force deletion")
	level := fs.String("level", "manage", "PAT authorization level to use (must be manage)")
	dryRun := fs.Bool("dry-run", false, "Lists matching pipelines without deleting them")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Permanently deletes classic release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: delete-pipelines <target> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"filter": true, "comment": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if *level != "manage" {
		fmt.Fprintln(os.Stderr, "Error: --level must be manage for permanent deletion")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *comment, *level, *force, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, filter, comment, level string, force, dryRun, autoYes bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, definitions, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(definitions) == 0 {
		fmt.Printf("No matching release pipelines found under `%s`.\n", target)
		return nil
	}

	fmt.Printf("Target: %s\nPipelines to delete: %d\n", target, len(definitions))
	for _, definition := range definitions {
		path := definition.Path
		if path == "" {
			path = `\`
		}
		fmt.Printf("  - %s\\%s (id=%d)\n", strings.TrimRight(path, `\`), definition.Name, definition.ID)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was deleted.")
		return nil
	}
	if !autoYes {
		fmt.Printf("\nPermanently delete %d pipeline(s)? This cannot be undone. (y/N): ", len(definitions))
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	failed := 0
	for _, definition := range definitions {
		if err := cfg.DeleteDefinition(project, definition.ID, comment, force); err != nil {
			fmt.Printf("  x %s: %v\n", definition.Name, err)
			failed++
			continue
		}
		fmt.Printf("  ok %s deleted\n", definition.Name)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d pipeline deletion(s) failed", failed, len(definitions))
	}
	fmt.Printf("\nDeleted %d pipeline(s).\n", len(definitions))
	return nil
}
