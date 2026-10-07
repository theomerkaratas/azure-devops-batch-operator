// Command audit-pipeline-policy audits classic release definitions against a YAML policy.
package auditpipelinepolicy

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
  audit-pipeline-policy 'Example.Project\TEST\CONFIG' --policy release-policy.yml
Arguments:
  target                Required. A folder or one full release pipeline path.
  --policy              Required. Path to the YAML policy file.
  --filter              Optional. Only pipeline names containing this text.
  --fail-on-violation   Return a failure when one or more violations are found (useful in CI).
  --level               Optional. PAT level (default: read).
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("audit-pipeline-policy", flag.ExitOnError)
	policyPath := fs.String("policy", "", "Path to the YAML policy file")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	failOnViolation := fs.Bool("fail-on-violation", false, "Returns an error when violations are found")
	level := fs.String("level", azuredevops.DefaultLevel("read"), "PAT authorization level to use: read, read-write, or manage")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Audits release pipelines under a path against a YAML policy.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: audit-pipeline-policy <target> --policy <file> [flags]")
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
	if err := run(fs.Arg(0), *policyPath, *filter, *level, *failOnViolation); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, policyPath, filter, level string, failOnViolation bool) error {
	policy, err := pipelinepolicy.Load(policyPath)
	if err != nil {
		return err
	}
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, definitions, err := batchupdate.SelectDefinitions(cfg, target, filter)
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
	fmt.Printf("Target: %s\nPolicy: %s\nPipelines found: %d\n", target, policyPath, len(definitions))
	violating, violations := 0, 0
	for _, definition := range definitions {
		raw, err := cfg.GetDefinitionDetailRaw(project, definition.ID)
		if err != nil {
			return fmt.Errorf("read %s: %w", definition.Name, err)
		}
		result := pipelinepolicy.Evaluate(raw, policy, queueID, false)
		if len(result.Violations) == 0 {
			continue
		}
		violating++
		violations += len(result.Violations)
		path := definition.Path
		if path == "" {
			path = `\`
		}
		fmt.Printf("\n[VIOLATION] %s\\%s (id=%d)\n", strings.TrimRight(path, `\`), definition.Name, definition.ID)
		for _, violation := range result.Violations {
			fmt.Printf("  - %s\n", violation)
		}
	}
	fmt.Printf("\nAudit complete: %d pipeline(s), %d violating, %d violation(s).\n", len(definitions), violating, violations)
	if failOnViolation && violations > 0 {
		return fmt.Errorf("policy audit found %d violation(s)", violations)
	}
	return nil
}
