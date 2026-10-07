// Command replace-pipeline-content performs regex replacements inside classic release definitions.
package replacepipelinecontent

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  replace-pipeline-content 'Example.Project\TEST' --find 'old-command' --replace 'new-command' --dry-run
  replace-pipeline-content 'Example.Project\TEST' --find 'Service-(\w+)' --replace 'App-$1' --fields titles,variables --yes
Arguments:
  target      Required. A folder (covers all pipelines under it) or one full pipeline path.
  --find      Required. Go regular expression to search for.
  --replace   Replacement text. Supports groups such as $1 and ${name}; empty deletes matches.
  --fields    Comma-separated fields to inspect (default: scripts,titles,variables).
              Choices: scripts | titles | variables | all
  --filter    Optional. Only pipeline names containing this text (case-insensitive).
  --level     Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run   Lists planned replacements without saving anything.
  -y, --yes   Skips the confirmation prompt.
`

var scriptInputKeys = map[string]bool{
	"script": true, "inlineScript": true,
	"powershellScript": true, "inline": true, "contents": true,
}

type targets struct{ scripts, titles, variables bool }

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("replace-pipeline-content", flag.ExitOnError)
	find := fs.String("find", "", "Go regular expression to search for")
	replacement := fs.String("replace", "", "Replacement text; supports $1 and ${name}")
	fields := fs.String("fields", "scripts,titles,variables", "Comma-separated fields: scripts,titles,variables,all")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level to use: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists planned replacements without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-replaces regex matches in scripts, step titles, and variables under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: replace-pipeline-content <target> --find <regex> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"find": true, "replace": true, "fields": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *find == "" {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	re, err := regexp.Compile(*find)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: invalid --find regular expression:", err)
		os.Exit(2)
	}
	selected, err := parseTargets(*fields)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}

	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		return replaceContent(raw, re, *replacement, selected)
	}
	if err := batchupdate.Run(opts, "Automated regex content replacement", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func parseTargets(value string) (targets, error) {
	var out targets
	for _, field := range strings.Split(value, ",") {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "all":
			out = targets{true, true, true}
		case "scripts":
			out.scripts = true
		case "titles":
			out.titles = true
		case "variables":
			out.variables = true
		case "":
		default:
			return targets{}, fmt.Errorf("unknown --fields value %q (choices: scripts, titles, variables, all)", field)
		}
	}
	if !out.scripts && !out.titles && !out.variables {
		return targets{}, fmt.Errorf("--fields must select at least one of: scripts, titles, variables")
	}
	return out, nil
}

func replaceContent(raw map[string]interface{}, re *regexp.Regexp, replacement string, selected targets) []string {
	var changes []string
	if selected.variables {
		changes = append(changes, replaceVariables(raw, "pipeline", re, replacement)...)
	}
	batchupdate.EachEnvironment(raw, "", func(stageName string, env map[string]interface{}) {
		if selected.variables {
			changes = append(changes, replaceVariables(env, "stage "+stageName, re, replacement)...)
		}
		phases, _ := env["deployPhases"].([]interface{})
		for _, phaseValue := range phases {
			phase, ok := phaseValue.(map[string]interface{})
			if !ok {
				continue
			}
			jobName, _ := phase["name"].(string)
			tasks, _ := phase["workflowTasks"].([]interface{})
			for _, taskValue := range tasks {
				task, ok := taskValue.(map[string]interface{})
				if !ok {
					continue
				}
				title, _ := task["name"].(string)
				label := stageName + " / " + jobName + " / " + title
				if selected.titles {
					if updated, count := replaceString(title, re, replacement); count > 0 {
						task["name"] = updated
						changes = append(changes, fmt.Sprintf("[%s / %s] title: %q -> %q (%d match(es))", stageName, jobName, title, updated, count))
					}
				}
				if selected.scripts {
					inputs, _ := task["inputs"].(map[string]interface{})
					for key := range scriptInputKeys {
						value, ok := inputs[key].(string)
						if !ok {
							continue
						}
						if updated, count := replaceString(value, re, replacement); count > 0 {
							inputs[key] = updated
							changes = append(changes, fmt.Sprintf("[%s] script input %s: %d match(es)", label, key, count))
						}
					}
				}
			}
		}
	})
	sort.Strings(changes)
	return changes
}

func replaceVariables(owner map[string]interface{}, scope string, re *regexp.Regexp, replacement string) []string {
	vars, _ := owner["variables"].(map[string]interface{})
	var changes []string
	for name, variableValue := range vars {
		variable, ok := variableValue.(map[string]interface{})
		if !ok {
			continue
		}
		value, ok := variable["value"].(string)
		if !ok {
			continue
		}
		if updated, count := replaceString(value, re, replacement); count > 0 {
			variable["value"] = updated
			changes = append(changes, fmt.Sprintf("[%s] variable %s: %d match(es)", scope, name, count))
		}
	}
	return changes
}

func replaceString(value string, re *regexp.Regexp, replacement string) (string, int) {
	count := len(re.FindAllStringIndex(value, -1))
	if count == 0 {
		return value, 0
	}
	updated := re.ReplaceAllString(value, replacement)
	if updated == value {
		return value, 0
	}
	return updated, count
}
