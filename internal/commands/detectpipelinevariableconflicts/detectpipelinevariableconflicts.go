// Command detect-pipeline-variable-conflicts reports duplicate or conflicting variable names
// across pipeline variables, stage variables, and linked variable groups, and shows which value
// takes precedence.
package detectpipelinevariableconflicts

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

const usageEpilog = `
Examples:
  detect-pipeline-variable-conflicts 'Example.Project\TEST'
  detect-pipeline-variable-conflicts 'Example.Project\TEST' --filter API --level read
Arguments:
  target     Required. Pipeline or folder path, in 'Project\Folder\...' format.
  --filter   Optional. Only pipeline names containing this text.
  --level    Optional. PAT authorization level to use (default: read).
             Choices: read | read-write | manage
Precedence (lowest to highest): linked variable groups (last-linked group wins among groups),
then pipeline variables, then stage variables. Variable groups linked at stage scope are applied
after the ones linked at pipeline scope.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("detect-pipeline-variable-conflicts", flag.ExitOnError)
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Reports duplicate or conflicting variable names across pipeline/stage variables and linked variable groups.")
		fmt.Fprintln(os.Stderr, "\nUsage: detect-pipeline-variable-conflicts <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// contribution is one variable definition competing for a name, in increasing precedence order
// within its scope's contribution list.
type contribution struct {
	source string
}

func run(target, filter, level string) error {
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
		return nil
	}

	groups, err := cfg.GetProjectVariableGroups(project)
	if err != nil {
		return fmt.Errorf("fetching variable groups: %w", err)
	}

	totalConflicts := 0
	pipelinesWithConflicts := 0
	for _, d := range defs {
		raw, err := cfg.GetDefinitionDetailRaw(project, d.ID)
		if err != nil {
			return fmt.Errorf("%s\\%s: %w", target, d.Name, err)
		}
		path := d.Path
		if path == "" {
			path = `\`
		}
		n := reportPipeline(cfg, project, path, d.Name, raw, groups)
		if n > 0 {
			pipelinesWithConflicts++
			totalConflicts += n
		}
	}

	fmt.Println("\n============================================================")
	fmt.Printf("Pipelines scanned: %d | With conflicts: %d | Total conflicts: %d\n", len(defs), pipelinesWithConflicts, totalConflicts)
	fmt.Println("============================================================")
	return nil
}

func reportPipeline(cfg azuredevops.Config, project, path, name string, raw map[string]interface{}, groups map[int]azuredevops.VariableGroup) int {
	pipelineGroupIDs := groupIDs(raw["variableGroups"])
	pipelineVars := varNames(raw["variables"])

	pipelineContributions := groupContributions(pipelineGroupIDs, groups)
	pipelineContributions = append(pipelineContributions, namedContributions(pipelineVars, "pipeline variable")...)

	header := fmt.Sprintf("\n--- %s\\%s (id=%d) ---", path, name, batchupdate.Int(raw, "id"))
	printedHeader := false
	total := 0

	if n := printConflicts(pipelineContributions, "pipeline scope", func() {
		if !printedHeader {
			fmt.Println(header)
			printedHeader = true
		}
	}); n > 0 {
		total += n
	}

	envs, _ := raw["environments"].([]interface{})
	for _, e := range envs {
		env, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		stageName, _ := env["name"].(string)
		stageGroupIDs := groupIDs(env["variableGroups"])
		stageVars := varNames(env["variables"])

		stageContributions := groupContributions(pipelineGroupIDs, groups)
		stageContributions = append(stageContributions, groupContributions(stageGroupIDs, groups)...)
		stageContributions = append(stageContributions, namedContributions(pipelineVars, "pipeline variable")...)
		stageContributions = append(stageContributions, namedContributions(stageVars, fmt.Sprintf("stage %q variable", stageName))...)

		if n := printConflicts(stageContributions, fmt.Sprintf("stage %q scope", stageName), func() {
			if !printedHeader {
				fmt.Println(header)
				printedHeader = true
			}
		}); n > 0 {
			total += n
		}
	}

	if total == 0 && printedHeader {
		fmt.Println("  (no conflicts)")
	}
	return total
}

// printConflicts groups contributions by variable name and prints every name defined more than
// once, naming every source and the one that wins (the last contribution in precedence order).
// onConflict is called once, lazily, right before the first line is printed for this pipeline.
func printConflicts(contributions []struct {
	name   string
	source string
}, scopeLabel string, onConflict func()) int {
	byName := map[string][]string{}
	order := []string{}
	for _, c := range contributions {
		if _, ok := byName[c.name]; !ok {
			order = append(order, c.name)
		}
		byName[c.name] = append(byName[c.name], c.source)
	}
	sort.Strings(order)

	count := 0
	for _, name := range order {
		sources := byName[name]
		if len(sources) < 2 {
			continue
		}
		onConflict()
		winner := sources[len(sources)-1]
		fmt.Printf("  [%s] %q defined %d times: %s\n", scopeLabel, name, len(sources), strings.Join(sources, " ; "))
		fmt.Printf("    -> effective value comes from: %s\n", winner)
		count++
	}
	return count
}

func groupContributions(ids []int, groups map[int]azuredevops.VariableGroup) []struct {
	name   string
	source string
} {
	var out []struct {
		name   string
		source string
	}
	for _, id := range ids {
		group, ok := groups[id]
		groupName := fmt.Sprintf("Group %d", id)
		if !ok {
			fmt.Fprintf(os.Stderr, "Warning: variable group %d is linked but not visible to this PAT/project; its variables were not checked for conflicts.\n", id)
			continue
		}
		groupName = group.Name
		names := make([]string, 0, len(group.Variables))
		for n := range group.Variables {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, struct {
				name   string
				source string
			}{name: n, source: fmt.Sprintf("variable group %q (id=%d)", groupName, id)})
		}
	}
	return out
}

func namedContributions(names []string, source string) []struct {
	name   string
	source string
} {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	out := make([]struct {
		name   string
		source string
	}, 0, len(sorted))
	for _, n := range sorted {
		out = append(out, struct {
			name   string
			source string
		}{name: n, source: source})
	}
	return out
}

func groupIDs(value interface{}) []int {
	items, _ := value.([]interface{})
	out := make([]int, 0, len(items))
	for _, item := range items {
		var n int
		switch v := item.(type) {
		case float64:
			n = int(v)
		case int:
			n = v
		}
		if n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func varNames(value interface{}) []string {
	m, _ := value.(map[string]interface{})
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	return out
}
