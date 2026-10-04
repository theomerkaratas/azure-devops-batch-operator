// Command rename-or-move-pipelines bulk-renames and/or moves Azure DevOps release pipelines
// to another folder within the same project.
package renameormovepipelines

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
  # Move every pipeline under a folder to another folder:
  rename-or-move-pipelines 'Example.Project\TEST\CONFIG' --move-to 'TEST\ARCHIVE' --dry-run
  # Rename by replacing text in names:
  rename-or-move-pipelines 'Example.Project\TEST\CONFIG' --find 'Example.' --replace 'Platform.'
  # Rename a single pipeline:
  rename-or-move-pipelines 'Example.Project\TEST\CONFIG\TESTAPP\Inspector' --name 'Inspector.v2' -y
Arguments:
  target      Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --move-to   Optional. Destination folder inside the same project, without the project name.
              E.g.: 'TEST\ARCHIVE'. Missing folders are created. Use '\' for the project root.
  --find      Optional. Text to find in pipeline names (with --replace).
  --replace   Optional. Replacement for --find (may be empty to delete the text).
  --name      Optional. New name; only valid when the target is a single pipeline.
              At least one of --move-to, --find or --name is required.
  --filter    Optional. Only pipelines whose name contains this text (case-insensitive).
  --level     Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run   Lists the changes without saving anything.
  -y, --yes   Skips the confirmation prompt.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("rename-or-move-pipelines", flag.ExitOnError)
	moveTo := fs.String("move-to", "", "Destination folder inside the same project")
	find := fs.String("find", "", "Text to find in pipeline names (with --replace)")
	replace := fs.String("replace", "", "Replacement for --find")
	name := fs.String("name", "", "New name; only for a single-pipeline target")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists the changes without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-renames and/or moves release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: rename-or-move-pipelines <target> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"move-to": true, "find": true, "replace": true, "name": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if *moveTo == "" && *find == "" && *name == "" {
		fmt.Fprintln(os.Stderr, "Error: specify at least one of --move-to, --find or --name.")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	dest := ""
	if *moveTo != "" {
		dest = normalizeFolder(*moveTo)
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if dest != "" && dest != `\` {
		opts.BeforeApply = func(cfg azuredevops.Config, project string) error {
			return cfg.CreateFolder(project, dest)
		}
	}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		var lines []string
		oldName, _ := raw["name"].(string)
		oldPath, _ := raw["path"].(string)
		if oldPath == "" {
			oldPath = `\`
		}
		newName := oldName
		if *name != "" {
			newName = *name
		} else if *find != "" {
			newName = strings.ReplaceAll(oldName, *find, *replace)
		}
		if newName != oldName {
			raw["name"] = newName
			lines = append(lines, fmt.Sprintf("rename: %s -> %s", oldName, newName))
		}
		if dest != "" && !strings.EqualFold(dest, oldPath) {
			raw["path"] = dest
			lines = append(lines, fmt.Sprintf("move  : %s -> %s", oldPath, dest))
		}
		return lines
	}
	if *name != "" {
		cfg, err := azuredevops.LoadConfig(*level)
		if err == nil {
			if _, defs, err := batchupdate.SelectDefinitions(cfg, fs.Arg(0), *filter); err == nil && len(defs) > 1 {
				fmt.Fprintln(os.Stderr, "Error: --name requires a target that matches a single pipeline.")
				os.Exit(2)
			}
		}
	}
	if err := batchupdate.Run(opts, "Automated rename/move", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// normalizeFolder turns 'A/B' or '\A\B\' into the API form '\A\B'.
func normalizeFolder(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) == 0 {
		return `\`
	}
	return `\` + strings.Join(parts, `\`)
}
