// Command create-project creates an Azure DevOps team project.
package createproject

import (
	"bufio"
	"context"
	"encoding/json"
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
  create-project 'Example.Project' --process-template Agile --dry-run
  create-project '["Project1", "Project 2"]' --dry-run
  create-project --name 'Example.Project' --description 'Team project' -y
Arguments:
  name                   Required. Single name or JSON array, positional or --name (not both).
  --visibility           private (default) | public
  --process-template     Process ID or name; omitted uses the organization default.
  --source-control-type  Git (default) | Tfvc
  --level                PAT level: read-write | manage
  --dry-run              Preview without calling the API.
  -y, --yes              Skip confirmation.
Options apply to every project. One confirmation covers the entire batch.
Projects are created sequentially, with up to 10 minutes waiting per project.
Stops on the first error; projects already created are kept.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("create-project", flag.ExitOnError)
	name := fs.String("name", "", "Project name or JSON array (or positional argument)")
	description := fs.String("description", "", "Project description")
	visibility := fs.String("visibility", "private", "Project visibility: private, public")
	process := fs.String("process-template", "", "Process template ID or name (default: organization default)")
	source := fs.String("source-control-type", "Git", "Source control: Git, Tfvc")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write, manage")
	dryRun := fs.Bool("dry-run", false, "Preview without calling the API")
	yes := fs.Bool("yes", false, "Skip confirmation")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Creates a new Azure DevOps team project\n\nUsage: create-project <name-or-json-array> [flags]\n\n")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"name": true, "description": true, "visibility": true, "process-template": true, "source-control-type": true, "level": true})); err != nil {
		os.Exit(2)
	}
	resolved, err := projectName(*name, fs.Args())
	if err == nil {
		var names []string
		names, err = parseNames(resolved)
		if err == nil {
			_, err = payload(names[0], *description, *visibility, *source, *process)
		}
	}
	if err == nil && !batchupdate.ValidWriteLevel(*level) {
		err = fmt.Errorf("--level must be one of: read-write, manage")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fs.Usage()
		os.Exit(2)
	}
	if err := run(resolved, *description, *visibility, *process, *source, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func projectName(name string, args []string) (string, error) {
	if len(args) > 1 || (len(args) > 0 && name != "") {
		return "", fmt.Errorf("provide one project name, positional or --name")
	}
	if len(args) == 1 {
		name = args[0]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("project name is required")
	}
	return name, nil
}

func payload(name, description, visibility, source, process string) (azuredevops.ProjectOptions, error) {
	if strings.TrimSpace(name) == "" {
		return azuredevops.ProjectOptions{}, fmt.Errorf("project name is required")
	}
	if visibility != "private" && visibility != "public" {
		return azuredevops.ProjectOptions{}, fmt.Errorf("--visibility must be private or public")
	}
	if source != "Git" && source != "Tfvc" {
		return azuredevops.ProjectOptions{}, fmt.Errorf("--source-control-type must be Git or Tfvc")
	}
	p := azuredevops.ProjectOptions{Name: name, Description: description, Visibility: visibility, Capabilities: map[string]map[string]string{"versioncontrol": {"sourceControlType": source}}}
	if process != "" {
		p.Capabilities["processTemplate"] = map[string]string{"templateTypeId": process}
	}
	return p, nil
}

func run(name, description, visibility, process, source, level string, dryRun, autoYes bool) error {
	names, err := parseNames(name)
	if err != nil {
		return err
	}
	p, err := payload(names[0], description, visibility, source, process)
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
	displayProcess := process
	if displayProcess == "" {
		displayProcess = "organization default"
	}
	for i, name := range names {
		fmt.Printf("Project %d/%d: %s\n", i+1, len(names), name)
	}
	fmt.Printf("Description: %s\nVisibility: %s\nSource control: %s\nProcess template: %s\n", description, visibility, source, displayProcess)
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was created.")
		return nil
	}
	if !autoYes {
		fmt.Printf("\nCreate %d project(s)? (y/N): ", len(names))
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	id, err := cfg.ResolveProjectProcess(strings.TrimSpace(process))
	if err != nil {
		return err
	}
	p.Capabilities["processTemplate"] = map[string]string{"templateTypeId": id}
	for i, name := range names {
		p.Name = name
		op, err := cfg.CreateProject(p)
		if err != nil {
			return fmt.Errorf("create project %q (%d/%d); %d project(s) completed; stopping batch: %w", name, i+1, len(names), i, err)
		}
		fmt.Printf("[%d/%d] %s: creation queued (operation %s). Waiting for completion...\n", i+1, len(names), name, op.ID)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		err = cfg.WaitProjectOperation(ctx, op, 2*time.Second)
		cancel()
		if err != nil {
			return fmt.Errorf("project %q (%d/%d); %d project(s) completed; stopping batch: %w", name, i+1, len(names), i, err)
		}
		fmt.Printf("Created project %s.\n", name)
	}
	fmt.Printf("\nCompleted: %d project(s) created.\n", len(names))
	return nil
}

// parseNames accepts a single name or a JSON array, preserving spaces inside names.
func parseNames(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	names := []string{input}
	if strings.HasPrefix(input, "[") {
		names = nil
		if err := json.Unmarshal([]byte(input), &names); err != nil {
			return nil, fmt.Errorf("project names must be a JSON array of strings: %w", err)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one project name is required")
	}
	seen := make(map[string]bool)
	for i, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("project name %d is empty", i+1)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("duplicate project name %q", name)
		}
		seen[key] = true
		names[i] = name
	}
	return names, nil
}
