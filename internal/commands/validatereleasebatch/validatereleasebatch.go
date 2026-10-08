// Command validate-release-batch checks whether a group of release pipelines is ready to run.
package validatereleasebatch

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type issue struct {
	severity string
	text     string
}

const usageEpilog = `
Examples:
  validate-release-batch 'Example.Project\TEST'
  validate-release-batch 'Example.Project\TEST' --stage Production --fail-on-issues
  validate-release-batch 'Example.Project\TEST' --allow-active --filter API
Checks:
  - every requested stage exists
  - pipelines contain stages and artifacts
  - build artifacts identify a build definition
  - agent jobs identify an agent queue
  - no recent release has a queued, scheduled, or in-progress stage (unless --allow-active)
ERROR findings make the batch unsafe. WARNING findings deserve review but do not fail
--fail-on-issues.
`

func Main() {
	fs := flag.NewFlagSet("validate-release-batch", flag.ExitOnError)
	stages := multiFlag{}
	fs.Var(&stages, "stage", "Stage that must exist. May be given multiple times.")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	top := fs.Int("top", 20, "Recent releases per pipeline to inspect for active deployments")
	allowActive := fs.Bool("allow-active", false, "Do not report active deployments as blockers")
	failOnIssues := fs.Bool("fail-on-issues", false, "Exit with status 1 when ERROR findings exist")
	level := fs.String("level", azuredevops.DefaultLevel("read"), "PAT authorization level: read, read-write or manage")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Validates a batch of release pipelines before releases are created.")
		fmt.Fprintln(os.Stderr, "\nUsage: validate-release-batch <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"stage": true, "filter": true, "top": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *top < 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, []string(stages), *top, *allowActive, *failOnIssues, *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func object(value interface{}) map[string]interface{} {
	m, _ := value.(map[string]interface{})
	return m
}

func list(value interface{}) []interface{} {
	v, _ := value.([]interface{})
	return v
}

func text(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func number(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func inspectDefinition(raw map[string]interface{}, requiredStages []string) []issue {
	var out []issue
	envs := list(raw["environments"])
	if len(envs) == 0 {
		out = append(out, issue{"ERROR", "pipeline has no stages"})
	}
	found := map[string]bool{}
	for _, value := range envs {
		env := object(value)
		found[strings.ToLower(text(env, "name"))] = true
		for _, phaseValue := range list(env["deployPhases"]) {
			phase := object(phaseValue)
			if !strings.EqualFold(text(phase, "phaseType"), "agentBasedDeployment") {
				continue
			}
			input := object(phase["deploymentInput"])
			if input == nil {
				out = append(out, issue{"ERROR", fmt.Sprintf("stage %q agent job %q has no deployment input", text(env, "name"), text(phase, "name"))})
			} else if number(input["queueId"]) == 0 {
				out = append(out, issue{"ERROR", fmt.Sprintf("stage %q job %q has no agent queue", text(env, "name"), text(phase, "name"))})
			}
		}
	}
	for _, required := range requiredStages {
		if !found[strings.ToLower(required)] {
			out = append(out, issue{"ERROR", fmt.Sprintf("requested stage %q does not exist", required)})
		}
	}

	artifacts := list(raw["artifacts"])
	if len(artifacts) == 0 {
		out = append(out, issue{"WARNING", "pipeline has no artifacts; a release may have nothing to deploy"})
	}
	for _, value := range artifacts {
		artifact := object(value)
		if !strings.EqualFold(text(artifact, "type"), "Build") {
			continue
		}
		alias := text(artifact, "alias")
		ref := object(artifact["definitionReference"])
		definition := object(ref["definition"])
		if definition == nil || (text(definition, "id") == "" && text(definition, "name") == "") {
			out = append(out, issue{"ERROR", fmt.Sprintf("build artifact %q has no build definition", alias)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].severity != out[j].severity {
			return out[i].severity < out[j].severity
		}
		return out[i].text < out[j].text
	})
	return out
}

func activeIssues(releases []azuredevops.Release) []issue {
	var out []issue
	for _, rel := range releases {
		for _, env := range rel.Environments {
			switch strings.ToLower(env.Status) {
			case "inprogress", "queued", "scheduled":
				out = append(out, issue{"ERROR", fmt.Sprintf("release %s has active stage %q (%s)", rel.Name, env.Name, env.Status)})
			}
		}
	}
	return out
}

func noMatchesError(failOnIssues bool) error {
	if failOnIssues {
		return fmt.Errorf("release batch validation found no matching pipelines")
	}
	return nil
}

func run(target, filter string, stages []string, top int, allowActive, failOnIssues bool, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under `%s`.\n", target)
		return noMatchesError(failOnIssues)
	}
	details, err := batchupdate.FetchDetails(cfg, project, defs)
	if err != nil {
		return err
	}
	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))
	errors, warnings, ready := 0, 0, 0
	for _, def := range defs {
		findings := inspectDefinition(details[def.ID], stages)
		if !allowActive {
			releases, err := cfg.ListReleases(project, def.ID, top)
			if err != nil {
				return fmt.Errorf("%s: inspect active releases: %w", def.Name, err)
			}
			findings = append(findings, activeIssues(releases)...)
		}
		if len(findings) == 0 {
			ready++
			fmt.Printf("  [READY] %s\\%s\n", strings.TrimRight(def.Path, `\`), def.Name)
			continue
		}
		fmt.Printf("\n%s\\%s (id=%d)\n", strings.TrimRight(def.Path, `\`), def.Name, def.ID)
		for _, finding := range findings {
			fmt.Printf("  [%s] %s\n", finding.severity, finding.text)
			if finding.severity == "ERROR" {
				errors++
			} else {
				warnings++
			}
		}
	}
	fmt.Printf("\nValidation complete: %d ready pipeline(s), %d error(s), %d warning(s).\n", ready, errors, warnings)
	if failOnIssues && errors > 0 {
		return fmt.Errorf("release batch validation found %d blocking error(s)", errors)
	}
	return nil
}
