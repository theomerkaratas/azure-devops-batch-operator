// Command delete-pipeline-steps removes exactly named tasks from Azure DevOps classic release pipelines.
package deletepipelinesteps

import (
	"flag"
	"fmt"
	"os"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  delete-pipeline-steps 'Example.Project\TEST\CONFIG' --title 'Obsolete deployment step' --dry-run
  delete-pipeline-steps 'Example.Project\TEST\CONFIG' --title 'Obsolete deployment step' --yes
Arguments:
  target     Required. A folder (covers all pipelines under it) or one full pipeline path.
  --title    Required. Step title to match exactly (case-sensitive).
  --filter   Optional. Only pipeline names containing this text (case-insensitive).
  --level    Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run  Lists matching steps without saving anything.
  -y, --yes  Skips the confirmation prompt.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("delete-pipeline-steps", flag.ExitOnError)
	title := fs.String("title", "", "Step title to match exactly (case-sensitive)")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level to use: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists matching steps without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Deletes steps with an exact title from release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: delete-pipeline-steps <target> --title <exact-title> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"title": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *title == "" {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}

	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		return removeStepsByExactTitle(raw, *title)
	}
	if err := batchupdate.Run(opts, "Automated exact-title step deletion", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// removeStepsByExactTitle removes all tasks whose name is exactly title and describes each removal.
func removeStepsByExactTitle(raw map[string]interface{}, title string) []string {
	var changes []string
	batchupdate.EachEnvironment(raw, "", func(stageName string, env map[string]interface{}) {
		phases, _ := env["deployPhases"].([]interface{})
		for _, value := range phases {
			phase, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			jobName, _ := phase["name"].(string)
			tasks, _ := phase["workflowTasks"].([]interface{})
			kept := make([]interface{}, 0, len(tasks))
			for _, taskValue := range tasks {
				task, ok := taskValue.(map[string]interface{})
				if !ok || task["name"] != title {
					kept = append(kept, taskValue)
					continue
				}
				changes = append(changes, fmt.Sprintf("[%s / %s] - step %q", stageName, jobName, title))
			}
			if len(kept) != len(tasks) {
				phase["workflowTasks"] = kept
			}
		}
	})
	return changes
}
