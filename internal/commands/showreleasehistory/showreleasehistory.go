// Command show-release-history shows the most recent releases of an Azure DevOps release pipeline.
package showreleasehistory

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Example:
  show-release-history 'Example.Project\PREP\CONFIG\PREPAPP\Example.API' --top 5
Arguments:
  pipeline_path  Required. Full pipeline path in 'Project\Folder\SubFolder\PipelineName' format.
  --top          Optional. Number of most recent releases to show (default: 10).
  --level        Optional. The PAT authorization level to use (default: read).
                 Choices: read | read-write | manage
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("show-release-history", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	top := fs.Int("top", 10, "number of most recent releases to show (default: 10)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Shows the most recent releases of a release pipeline: who created them, when, and stage statuses.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: show-release-history <pipeline_path> [--top N] [--level read|read-write|manage]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"level": true, "top": true})); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *top < 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *level, *top); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(pipelinePath, level string, top int) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, path, name, err := azuredevops.ParsePipelinePath(pipelinePath)
	if err != nil {
		return err
	}
	definition, err := cfg.FindDefinition(project, path, name)
	if err != nil {
		return err
	}
	releases, err := cfg.ListReleases(project, definition.ID, top)
	if err != nil {
		return err
	}
	fmt.Printf("Pipeline: %s  (id=%d)\n", definition.Name, definition.ID)
	if len(releases) == 0 {
		fmt.Println("No releases found.")
		return nil
	}
	for _, r := range releases {
		fmt.Printf("\n%s  [%s]  created %s by %s (%s)\n", r.Name, r.Status, r.CreatedOn, r.CreatedBy.DisplayName, r.Reason)
		for _, e := range r.Environments {
			fmt.Printf("  %-30s %s\n", e.Name, e.Status)
		}
	}
	return nil
}
