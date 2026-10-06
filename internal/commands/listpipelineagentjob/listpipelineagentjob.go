// Command list-pipeline-agent-job lists the agent job settings (pool, demands, timeout, etc.)
// of an Azure DevOps release pipeline.
package listpipelineagentjob

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
  list-pipeline-agent-job 'Example.Project\PREP\CONFIG\PREPAPP\Example.API' --level read
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

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-pipeline-agent-job", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists the agent job settings (pool, demands, timeout, etc.) of a release pipeline.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: list-pipeline-agent-job <pipeline_path> [--level read|read-write|manage]")
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
	project, _, _, err := azuredevops.ParsePipelinePath(pipelinePath)
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
			di := phase.DeploymentInput
			fmt.Printf("  Job: %s\n", phase.Name)
			if di == nil {
				fmt.Println("    (no deployment input)")
				continue
			}
			poolName := cfg.ResolvePoolName(project, di.QueueID)
			demands := "None"
			if len(di.Demands) > 0 {
				demands = fmt.Sprintf("%v", di.Demands)
			}
			fmt.Printf("    Agent pool: %s (queueId=%d)\n", poolName, di.QueueID)
			fmt.Printf("    Demands: %s\n", demands)
			fmt.Printf("    Parallel execution: %s\n", di.ParallelExecution.ParallelExecutionType)
			fmt.Printf("    Timeout (min): %d\n", di.TimeoutInMinutes)
			fmt.Printf("    Job cancel timeout (min): %d\n", di.JobCancelTimeoutInMinutes)
			fmt.Printf("    Skip artifacts download: %v\n", di.SkipArtifactsDownload)
			fmt.Printf("    Condition: %s\n", di.Condition)
		}
	}
	return nil
}
