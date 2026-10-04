// Command show-pipeline-steps shows the stages/tasks and script contents of an Azure DevOps release pipeline.
package showpipelinesteps

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
  show-pipeline-steps 'Example.Project\PREP\CONFIG\PREPAPP\Example.API' --level read
Arguments:
  pipeline_path  Required. Full pipeline path in 'Project\Folder\SubFolder\PipelineName' format.
                 E.g.: Example.Project\PREP\CONFIG\PREPAPP\Example.API
  --level        Optional. The PAT authorization level to use (default: read).
                 Choices: read | read-write | manage
                   read        -> Read-only viewing (sufficient and recommended for this command).
                   read-write  -> For commands that update pipeline definitions.
                   manage      -> For advanced definition/queue management operations.
                 Each level uses its own PAT environment variable (see README).
`

// scriptInputKeys lists the task input keys that may hold an inline script body, in priority order.
var scriptInputKeys = []string{"script", "inlineScript", "scriptSource", "powershellScript", "inline", "contents"}

func printTaskDetails(task azuredevops.WorkflowTask, indent string) {
	var flags []string
	if task.Condition != "" {
		flags = append(flags, "condition: "+task.Condition)
	}
	if task.ContinueOnError {
		flags = append(flags, "continueOnError: true")
	}
	if task.AlwaysRun {
		flags = append(flags, "alwaysRun: true")
	}
	if task.TimeoutInMinutes != 0 {
		flags = append(flags, fmt.Sprintf("timeout: %dm", task.TimeoutInMinutes))
	}
	if len(flags) > 0 {
		fmt.Printf("%sProperties: %s\n", indent, strings.Join(flags, ", "))
	}
	if len(task.Environment) > 0 {
		keys := make([]string, 0, len(task.Environment))
		for k := range task.Environment {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, task.Environment[k]))
		}
		fmt.Printf("%sEnv Vars: %s\n", indent, strings.Join(pairs, ", "))
	}
	if len(task.Inputs) == 0 {
		return
	}
	var scriptContent interface{}
	var scriptKeyUsed string
	for _, k := range scriptInputKeys {
		if v, ok := task.Inputs[k]; ok && fmt.Sprintf("%v", v) != "" {
			scriptContent = v
			scriptKeyUsed = k
			break
		}
	}
	var otherKeys []string
	for k, v := range task.Inputs {
		if k == scriptKeyUsed {
			continue
		}
		if strings.TrimSpace(fmt.Sprintf("%v", v)) != "" {
			otherKeys = append(otherKeys, k)
		}
	}
	if len(otherKeys) > 0 {
		sort.Strings(otherKeys)
		fmt.Printf("%sInputs:\n", indent)
		for _, k := range otherKeys {
			fmt.Printf("%s  - %s: %v\n", indent, k, task.Inputs[k])
		}
	}
	if scriptContent != nil {
		fmt.Printf("%sScript (%s):\n", indent, scriptKeyUsed)
		sep := indent + "------------------------------------------------------------"
		fmt.Println(sep)
		for _, line := range strings.Split(fmt.Sprintf("%v", scriptContent), "\n") {
			fmt.Printf("%s%s\n", indent, line)
		}
		fmt.Println(sep)
	}
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("show-pipeline-steps", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Shows the stages/tasks and script contents of an Azure DevOps release pipeline.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: show-pipeline-steps <pipeline_path> [--level read|read-write|manage]")
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
	pipelinePath := fs.Arg(0)
	if err := run(pipelinePath, *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
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
	for _, env := range definition.Environments {
		fmt.Printf("\nStage: %s\n", env.Name)
		for _, phase := range env.DeployPhases {
			fmt.Printf("  Job: %s  (phaseType=%s)\n", phase.Name, phase.PhaseType)
			for idx, task := range phase.WorkflowTasks {
				status := "enabled"
				if !task.Enabled {
					status = "DISABLED"
				}
				fmt.Printf("\n    %d. %s [%s] (taskId=%s, version=%s)\n", idx+1, task.Name, status, task.TaskID, task.Version)
				printTaskDetails(task, "       ")
			}
		}
	}
	return nil
}
