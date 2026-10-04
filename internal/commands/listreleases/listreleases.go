// Command list-releases lists Azure DevOps release pipelines and their folders.
package listreleases

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  list-releases Example.Project --level read
  list-releases 'Example.Project\TEST\CONFIG' --level read
Arguments:
  target   Required. A project name, or a folder path under a project.
           E.g.: 'Example.Project', 'Example.Project\TEST', 'Example.Project/DEV/CONFIG'.
           If only a project name is given, all folders and pipelines under it are listed.
  --level  Optional. The PAT authorization level to use (default: read).
           Choices: read | read-write | manage
             read        -> Read-only listing/viewing (sufficient and recommended for this command).
             read-write  -> For commands that update pipeline definitions.
             manage      -> For advanced definition/queue management operations.
           Each level uses its own PAT environment variable (see README).
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-releases", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists Azure DevOps release pipelines and their folders.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: list-releases <target> [--level read|read-write|manage]")
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
	project, subpathFilter, err := azuredevops.ParseTarget(target)
	if err != nil {
		return err
	}
	definitions, err := cfg.ListReleaseDefinitions(project)
	if err != nil {
		return err
	}
	folders := azuredevops.GroupByFolder(definitions, subpathFilter)
	if len(folders) == 0 {
		fmt.Printf("No release pipelines found under '%s'.\n", target)
		return nil
	}
	for _, folder := range azuredevops.SortedFolderNames(folders) {
		fmt.Println(folder)
		names := append([]string(nil), folders[folder]...)
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("  - %s\n", name)
		}
	}
	return nil
}
