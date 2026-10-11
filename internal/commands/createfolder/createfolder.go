// Command create-folder creates Azure DevOps release pipeline folders.
package createfolder

import (
	"bufio"
	"encoding/json"
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
  create-folder 'Folder1' --project 'Example.Project' --dry-run
  create-folder '["Folder1", "Folder 2"]' --project 'Example.Project' -y
  create-folder --path 'Deploy\Services' --project 'Example.Project' -y
Arguments:
  path         Required. Folder path or JSON array, positional or --path (not both).
               In JSON, escape backslashes: ["Deploy\\Services", "Test"].
  --project    Required. Existing Azure DevOps project containing the folders.
  --level      PAT level: read-write | manage
  --dry-run    Preview without calling the API.
  -y, --yes    Skip the batch confirmation.
Creates release pipeline folders, not repository directories or local folders.
Folders are created sequentially. Existing folders are accepted.
Stops on the first error; folders already created are kept.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("create-folder", flag.ExitOnError)
	project := fs.String("project", "", "Existing Azure DevOps project")
	path := fs.String("path", "", "Folder path or JSON array (or positional argument)")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write, manage")
	dryRun := fs.Bool("dry-run", false, "Preview without calling the API")
	yes := fs.Bool("yes", false, "Skip confirmation")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Creates Azure DevOps release pipeline folders.\n\nUsage: create-folder <path-or-json-array> --project <project> [flags]\n\n")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"project": true, "path": true, "level": true})); err != nil {
		os.Exit(2)
	}
	input, err := folderInput(*path, fs.Args())
	if err == nil {
		_, err = parsePaths(input)
	}
	if err == nil && strings.TrimSpace(*project) == "" {
		err = fmt.Errorf("--project is required")
	}
	if err == nil && !batchupdate.ValidWriteLevel(*level) {
		err = fmt.Errorf("--level must be one of: read-write, manage")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fs.Usage()
		os.Exit(2)
	}
	if err := run(*project, input, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func folderInput(path string, args []string) (string, error) {
	if len(args) > 1 || (len(args) > 0 && path != "") {
		return "", fmt.Errorf("provide a folder path or JSON array, positional or --path")
	}
	if len(args) == 1 {
		path = args[0]
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("folder path is required")
	}
	return path, nil
}

func parsePaths(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	paths := []string{input}
	if strings.HasPrefix(input, "[") {
		paths = nil
		if err := json.Unmarshal([]byte(input), &paths); err != nil {
			return nil, fmt.Errorf("folder paths must be a JSON array of strings: %w", err)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one folder path is required")
	}
	seen := map[string]bool{}
	for i, path := range paths {
		path = strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "/", `\`), `\`)
		if path == "" {
			return nil, fmt.Errorf("folder path %d is empty or refers to the project root", i+1)
		}
		for _, part := range strings.Split(path, `\`) {
			if strings.TrimSpace(part) == "" || part == "." || part == ".." {
				return nil, fmt.Errorf("invalid folder path %q", path)
			}
		}
		path = `\` + path
		key := strings.ToLower(path)
		if seen[key] {
			return nil, fmt.Errorf("duplicate folder path %q", path)
		}
		seen[key] = true
		paths[i] = path
	}
	return paths, nil
}

func run(project, input, level string, dryRun, autoYes bool) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	paths, err := parsePaths(input)
	if err != nil {
		return err
	}
	if !batchupdate.ValidWriteLevel(level) {
		return fmt.Errorf("--level must be one of: read-write, manage")
	}
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	fmt.Printf("Project: %s\n", project)
	for i, path := range paths {
		fmt.Printf("Folder %d/%d: %s\n", i+1, len(paths), path)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was created.")
		return nil
	}
	if !autoYes {
		fmt.Printf("\nCreate %d folder(s)? (y/N): ", len(paths))
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	completed, err := cfg.CreateFolders(project, paths)
	if err != nil {
		return err
	}
	fmt.Printf("\nCompleted: %d folder(s) created or already present.\n", completed)
	return nil
}
