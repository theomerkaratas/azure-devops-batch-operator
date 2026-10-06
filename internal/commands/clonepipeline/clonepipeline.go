// Command clone-pipeline copies an Azure DevOps release pipeline to a new name and/or folder
// within the same project.
package clonepipeline

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
  clone-pipeline 'Example.Project\DEV\CONFIG\DEVAPP\Example.Service' 'Example.Project\TEST\CONFIG\TESTAPP\Example.Service' --dry-run
  clone-pipeline 'Example.Project\DEV\CONFIG\App\Svc' 'Example.Project\DEV\CONFIG\App\Svc.copy' -y
Arguments:
  source       Required. Full path of the pipeline to copy: 'Project\Folder\PipelineName'.
  destination  Required. Full path of the new pipeline: 'Project\Folder\NewName'.
               It must be in the same project, and must not exist yet. A missing folder is created.
  --level      Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run    Lists what would be created without creating anything.
  -y, --yes    Skips the confirmation prompt.
Note: secret variable values are not returned by Azure DevOps, so the clone's secret variables
are empty and must be set again (see update-pipeline-variables --secret).
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("clone-pipeline", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists what would be created without creating anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Copies a release pipeline to a new name and/or folder in the same project.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: clone-pipeline <source> <destination> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"level": true})); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 2 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), fs.Arg(1), *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(source, destination, level string, dryRun, autoYes bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	srcProject, srcPath, srcName, err := azuredevops.ParsePipelinePath(source)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	dstProject, dstPath, dstName, err := azuredevops.ParsePipelinePath(destination)
	if err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if !strings.EqualFold(srcProject, dstProject) {
		return fmt.Errorf("destination must be in the same project (%s)", srcProject)
	}
	if _, err := cfg.FindDefinition(dstProject, dstPath, dstName); err == nil {
		return fmt.Errorf("destination already exists: %s", destination)
	}
	def, err := cfg.FindDefinition(srcProject, srcPath, srcName)
	if err != nil {
		return err
	}
	raw, err := cfg.GetDefinitionDetailRaw(srcProject, def.ID)
	if err != nil {
		return err
	}
	// Drop server-assigned identity so the API creates a fresh definition.
	for _, k := range []string{"id", "revision", "createdBy", "createdOn", "modifiedBy", "modifiedOn", "url", "_links", "lastRelease"} {
		delete(raw, k)
	}
	if envs, ok := raw["environments"].([]interface{}); ok {
		for _, e := range envs {
			if env, ok := e.(map[string]interface{}); ok {
				env["id"] = 0
			}
		}
	}
	raw["name"] = dstName
	raw["path"] = dstPath
	fmt.Printf("Source     : %s\\%s (id=%d)\n", srcPath, srcName, def.ID)
	fmt.Printf("Destination: %s\\%s\n", dstPath, dstName)
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was created.")
		return nil
	}
	if !autoYes {
		fmt.Print("\nCreate the clone? (y/N): ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if dstPath != `\` {
		if err := cfg.CreateFolder(dstProject, dstPath); err != nil {
			return fmt.Errorf("create folder %s: %w", dstPath, err)
		}
	}
	created, err := cfg.CreateDefinitionRaw(dstProject, raw, "Cloned from "+source)
	if err != nil {
		return err
	}
	fmt.Printf("\nCreated %s\\%s (id=%v)\n", dstPath, dstName, created["id"])
	return nil
}
