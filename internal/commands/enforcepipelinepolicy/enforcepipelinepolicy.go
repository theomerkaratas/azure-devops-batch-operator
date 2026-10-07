// Command enforce-pipeline-policy fixes classic release definitions to comply with a YAML policy.
package enforcepipelinepolicy

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
	"github.com/omerkaratas/azure-devops-go-automations/internal/pipelinepolicy"
)

const usageEpilog = `
Example:
  enforce-pipeline-policy 'Example.Project\TEST\CONFIG' --policy release-policy.yml --dry-run
Arguments:
  target      Required. A folder or one full release pipeline path.
  --policy    Required. Path to the YAML policy file.
  --filter    Optional. Only pipeline names containing this text.
  --level     Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run   Lists planned fixes without saving anything.
  -y, --yes   Skips the confirmation prompt.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("enforce-pipeline-policy", flag.ExitOnError)
	policyPath := fs.String("policy", "", "Path to the YAML policy file")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level to use: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists planned fixes without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Enforces a YAML policy across release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: enforce-pipeline-policy <target> --policy <file> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"policy": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *policyPath == "" {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *policyPath, *filter, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, policyPath, filter, level string, dryRun, autoYes bool) error {
	policy, err := pipelinepolicy.Load(policyPath)
	if err != nil {
		return err
	}
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, _, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	queueID := 0
	if policy.RequiredAgentPool != "" {
		queueID, err = cfg.ResolveQueueID(project, policy.RequiredAgentPool)
		if err != nil {
			return err
		}
	}
	var unresolved []string
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		result := pipelinepolicy.Evaluate(raw, policy, queueID, true)
		name, _ := raw["name"].(string)
		for _, violation := range result.Violations {
			if strings.Contains(violation, "audit-only") {
				unresolved = append(unresolved, name+": "+violation)
			}
		}
		return result.Changes
	}
	opts := batchupdate.Options{Target: target, Filter: filter, Level: level, DryRun: dryRun, AutoYes: autoYes}
	if err := batchupdate.Run(opts, "Automated pipeline policy enforcement", mutate); err != nil {
		return err
	}
	if len(unresolved) > 0 {
		fmt.Println("\nUnresolved audit-only violations:")
		for _, item := range unresolved {
			fmt.Printf("  - %s\n", item)
		}
	}
	return nil
}
