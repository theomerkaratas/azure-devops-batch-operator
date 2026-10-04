// Command update-pipeline-agent-job bulk-updates the agent job settings (timeouts, agent pool)
// of Azure DevOps release pipelines under a given path.
package updatepipelineagentjob

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
  update-pipeline-agent-job 'Example.Project\TEST\CONFIG' --timeout 120 --dry-run
  update-pipeline-agent-job 'Example.Project\TEST\CONFIG' --pool 'TestPool' --stage Development
  update-pipeline-agent-job 'Example.Project\TEST\CONFIG' --queue-id 42 -y
Arguments:
  target                Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --timeout             Optional. Job timeout in minutes (0 = no timeout).
  --job-cancel-timeout  Optional. Job cancel timeout in minutes.
  --pool                Optional. Agent pool name; resolved to a queue id from the project's build
                        definitions. If it can't be resolved, use --queue-id.
  --queue-id            Optional. Agent queue id to use directly.
                        At least one of the four settings above is required.
  --stage               Optional. Only update jobs under the given stage name.
  --filter              Optional. Only pipelines whose name contains this text (case-insensitive).
  --level               Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run             Lists the changes without saving anything.
  -y, --yes             Skips the confirmation prompt.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-agent-job", flag.ExitOnError)
	timeout := fs.Int("timeout", -1, "Job timeout in minutes (0 = no timeout)")
	cancelTimeout := fs.Int("job-cancel-timeout", -1, "Job cancel timeout in minutes")
	pool := fs.String("pool", "", "Agent pool name to switch to")
	queueID := fs.Int("queue-id", 0, "Agent queue id to switch to")
	stage := fs.String("stage", "", "Only update jobs under the given stage name")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists the changes without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-updates the agent job settings (timeouts, agent pool) of release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: update-pipeline-agent-job <target> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"timeout": true, "job-cancel-timeout": true, "pool": true, "queue-id": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if *timeout < 0 && *cancelTimeout < 0 && *pool == "" && *queueID == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify at least one of --timeout, --job-cancel-timeout, --pool, --queue-id.")
		os.Exit(2)
	}
	if *pool != "" && *queueID != 0 {
		fmt.Fprintln(os.Stderr, "Error: use either --pool or --queue-id, not both.")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	mutate := func(cfg azuredevops.Config, project string, raw map[string]interface{}) []string {
		newQueue := *queueID
		if *pool != "" {
			newQueue = resolveQueue(cfg, project, *pool)
			if newQueue == 0 {
				fmt.Fprintf(os.Stderr, "Warning: pool %q not found in project %s; skipping pool change (use --queue-id).\n", *pool, project)
				return nil
			}
		}
		var lines []string
		batchupdate.EachDeploymentInput(raw, *stage, func(stageName, job string, di map[string]interface{}) {
			prefix := fmt.Sprintf("[%s / %s]", stageName, job)
			setInt := func(key, label string, v int) {
				if old := batchupdate.Int(di, key); old != v {
					di[key] = v
					lines = append(lines, fmt.Sprintf("%s %s: %d -> %d", prefix, label, old, v))
				}
			}
			if *timeout >= 0 {
				setInt("timeoutInMinutes", "timeout (min)", *timeout)
			}
			if *cancelTimeout >= 0 {
				setInt("jobCancelTimeoutInMinutes", "job cancel timeout (min)", *cancelTimeout)
			}
			if newQueue != 0 {
				setInt("queueId", "queue id", newQueue)
			}
		})
		return lines
	}
	if err := batchupdate.Run(opts, "Automated agent job update", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// resolveQueue maps a pool name to its queue id using the project's known queues; 0 if unknown.
func resolveQueue(cfg azuredevops.Config, project, pool string) int {
	for id, name := range cfg.GetProjectQueues(project) {
		if strings.EqualFold(name, pool) {
			return id
		}
	}
	return 0
}
