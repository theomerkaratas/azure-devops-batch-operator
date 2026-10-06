// Command list-pipeline-variables lists the pipeline-level and stage-level variables of an
// Azure DevOps release pipeline.
package listpipelinevariables

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
Example:
  list-pipeline-variables 'Example.Project\PREP\CONFIG\PREPAPP\Example.API' --level read
Arguments:
  pipeline_path  Required. Full pipeline path in 'Project\Folder\SubFolder\PipelineName' format.
  --level        Optional. The PAT authorization level to use (default: read).
                 Choices: read | read-write | manage
                 Secret variable values are never returned by Azure DevOps and are listed as ***.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-pipeline-variables", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists the pipeline-level and stage-level variables of a release pipeline.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: list-pipeline-variables <pipeline_path> [--level read|read-write|manage]")
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
	if err := run(fs.Arg(0), *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func printVariables(indent string, vars map[string]azuredevops.ConfigVariable) {
	if len(vars) == 0 {
		fmt.Printf("%s(none)\n", indent)
		return
	}
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := vars[n]
		value := v.Value
		if v.IsSecret {
			value = "***"
		}
		fmt.Printf("%s%s = %s\n", indent, n, value)
	}
}
func run(pipelinePath, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	definition, err := cfg.ResolveDefinition(pipelinePath)
	if err != nil {
		return err
	}
	fmt.Printf("Pipeline: %s  (id=%d)\n", definition.Name, definition.ID)
	fmt.Println("\nPipeline variables:")
	printVariables("  ", definition.Variables)
	for _, env := range definition.Environments {
		fmt.Printf("\nStage: %s\n", env.Name)
		printVariables("  ", env.Variables)
	}
	return nil
}
