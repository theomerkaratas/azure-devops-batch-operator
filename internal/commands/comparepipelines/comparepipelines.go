// Command compare-pipelines diffs two Azure DevOps release pipelines (variables, agent job
// settings and tasks).
package comparepipelines

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

// flatten turns a definition into a key -> value map so two definitions can be diffed generically.
func flatten(cfg azuredevops.Config, pipelinePath string) (map[string]string, error) {
	project, _, _, err := azuredevops.ParsePipelinePath(pipelinePath)
	if err != nil {
		return nil, err
	}
	def, err := cfg.ResolveDefinition(pipelinePath)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	addVars := func(prefix string, vars map[string]azuredevops.ConfigVariable) {
		for n, v := range vars {
			if v.IsSecret {
				out[prefix+"variable "+n] = "(secret)"
			} else {
				out[prefix+"variable "+n] = v.Value
			}
		}
	}
	addVars("", def.Variables)
	for _, env := range def.Environments {
		sp := "stage " + env.Name + ": "
		out[sp+"exists"] = "yes"
		addVars(sp, env.Variables)
		for _, phase := range env.DeployPhases {
			jp := sp + "job " + phase.Name + ": "
			if di := phase.DeploymentInput; di != nil {
				out[jp+"agent pool"] = cfg.ResolvePoolName(project, di.QueueID)
				out[jp+"demands"] = strings.Join(di.Demands, "; ")
				out[jp+"timeout (min)"] = fmt.Sprint(di.TimeoutInMinutes)
				out[jp+"condition"] = di.Condition
			}
			for i, t := range phase.WorkflowTasks {
				out[fmt.Sprintf("%stask %02d", jp, i+1)] = fmt.Sprintf("%s (enabled=%v)", t.Name, t.Enabled)
			}
		}
	}
	return out, nil
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
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	fmt.Printf("A: %s\nB: %s\n\n", pathA, pathB)
	diffs := 0
	for _, k := range sorted {
		va, inA := a[k]
		vb, inB := b[k]
		switch {
		case inA && !inB:
			fmt.Printf("- only in A  %s = %s\n", k, va)
		case !inA && inB:
			fmt.Printf("+ only in B  %s = %s\n", k, vb)
		case va != vb:
			fmt.Printf("~ differs    %s\n    A: %s\n    B: %s\n", k, va, vb)
		default:
			continue
		}
		diffs++
	}
	if diffs == 0 {
		fmt.Println("No differences found.")
	} else {
		fmt.Printf("\n%d difference(s).\n", diffs)
	}
	return nil
}
