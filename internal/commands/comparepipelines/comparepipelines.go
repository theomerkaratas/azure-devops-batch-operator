// Command compare-pipelines diffs two Azure DevOps release pipelines (variables, agent job
// settings and tasks).
package comparepipelines

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
Example:
  compare-pipelines 'Example.Project\DEV\CONFIG\DEVAPP\Example.Service' 'Example.Project\TEST\CONFIG\TESTAPP\Example.Service'
Arguments:
  pipeline_a, pipeline_b  Required. Full pipeline paths in 'Project\Folder\SubFolder\PipelineName' format.
  --level                 Optional. The PAT authorization level to use (default: read).
                          Choices: read | read-write | manage
                          Secret variable values are not returned by Azure DevOps and are not compared.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("compare-pipelines", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Compares two release pipelines: variables, agent job settings and tasks.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: compare-pipelines <pipeline_a> <pipeline_b> [--level read|read-write|manage]")
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
	if err := run(fs.Arg(0), fs.Arg(1), *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// flatten resolves a pipeline path and turns its definition into a key -> value map, so two
// definitions can be diffed generically.
func flatten(cfg azuredevops.Config, pipelinePath string) (map[string]string, error) {
	project, _, _, err := azuredevops.ParsePipelinePath(pipelinePath)
	if err != nil {
		return nil, err
	}
	def, err := cfg.ResolveDefinition(pipelinePath)
	if err != nil {
		return nil, err
	}
	return batchupdate.FlattenDetail(cfg, project, def), nil
}

func run(pathA, pathB, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	a, err := flatten(cfg, pathA)
	if err != nil {
		return fmt.Errorf("%s: %w", pathA, err)
	}
	b, err := flatten(cfg, pathB)
	if err != nil {
		return fmt.Errorf("%s: %w", pathB, err)
	}
	fmt.Printf("A: %s\nB: %s\n\n", pathA, pathB)
	diffs := batchupdate.DiffFlat("A", "B", a, b)
	if diffs == 0 {
		fmt.Println("No differences found.")
	} else {
		fmt.Printf("\n%d difference(s).\n", diffs)
	}
	return nil
}
