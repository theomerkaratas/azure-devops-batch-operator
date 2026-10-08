// Command update-pipeline-cd-triggers enables, disables or modifies continuous-deployment triggers
// that create releases when a new build artifact is published.
package updatepipelinecdtriggers

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

type obj = map[string]interface{}

const usageEpilog = `
Examples:
  update-pipeline-cd-triggers 'Example.Project\TEST' --action enable --branch refs/heads/main,refs/heads/release/* --dry-run
  update-pipeline-cd-triggers 'Example.Project\TEST' --action disable --alias _Build -y
  update-pipeline-cd-triggers 'Example.Project\TEST' --action set-filters --branch refs/heads/main --tag release -y
Actions:
  enable       Turns the trigger on for the artifacts (creating it) and applies any given filters.
  disable      Removes the trigger from the artifacts.
  set-filters  Replaces the branch/tag filters of triggers that already exist; use with --branch/--tag
               (give neither to clear the filters so every build triggers a release).
--alias limits the change to the comma-separated artifact aliases (default: every Build artifact).
--branch takes comma-separated build branches (e.g. refs/heads/main); --tag takes comma-separated build tags.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-cd-triggers", flag.ExitOnError)
	action := fs.String("action", "", "enable, disable or set-filters")
	aliasList := fs.String("alias", "", "Comma-separated artifact aliases (default: all Build artifacts)")
	branchList := fs.String("branch", "", "Comma-separated build branch filters")
	tagList := fs.String("tag", "", "Comma-separated build tag filters")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists planned changes without saving")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Enables, disables or modifies continuous-deployment triggers across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-cd-triggers <target> --action <action> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"action": true, "alias": true, "branch": true, "tag": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	act := strings.ToLower(*action)
	branches, tags := splitNames(*branchList), splitNames(*tagList)
	switch {
	case fs.NArg() != 1:
		fs.Usage()
		os.Exit(2)
	case act != "enable" && act != "disable" && act != "set-filters":
		fmt.Fprintln(os.Stderr, "Error: --action must be one of: enable, disable, set-filters")
		os.Exit(2)
	case act == "disable" && (len(branches) > 0 || len(tags) > 0):
		fmt.Fprintln(os.Stderr, "Error: --branch and --tag do not apply to disable")
		os.Exit(2)
	case !batchupdate.ValidWriteLevel(*level):
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	aliases := splitNames(*aliasList)
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		lines, err := apply(raw, act, aliases, branches, tags)
		if err != nil {
			name, _ := raw["name"].(string)
			fmt.Printf("  - skipped %s: %v\n", name, err)
			return nil
		}
		return lines
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Updated continuous-deployment triggers: "+act, mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func splitNames(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func buildAliases(raw obj) []string {
	var out []string
	items, _ := raw["artifacts"].([]interface{})
	for _, item := range items {
		a, _ := item.(obj)
		if t, _ := a["type"].(string); strings.EqualFold(t, "Build") {
			if alias, _ := a["alias"].(string); alias != "" {
				out = append(out, alias)
			}
		}
	}
	return out
}

func conditions(branches, tags []string) []interface{} {
	tagList := make([]interface{}, len(tags))
	for i, t := range tags {
		tagList[i] = t
	}
	out := []interface{}{}
	for _, b := range branches {
		out = append(out, obj{"sourceBranch": b, "tags": append([]interface{}(nil), tagList...), "useBuildDefinitionBranch": false, "createReleaseOnBuildTagging": false})
	}
	if len(branches) == 0 && len(tags) > 0 {
		out = append(out, obj{"sourceBranch": "", "tags": tagList, "useBuildDefinitionBranch": true, "createReleaseOnBuildTagging": false})
	}
	return out
}

// filterKey summarizes a trigger's conditions for comparison and display.
func filterKey(trigger obj) string {
	items, _ := trigger["triggerConditions"].([]interface{})
	var parts []string
	for _, item := range items {
		c, _ := item.(obj)
		branch, _ := c["sourceBranch"].(string)
		tags, _ := c["tags"].([]interface{})
		var names []string
		for _, t := range tags {
			names = append(names, fmt.Sprint(t))
		}
		sort.Strings(names)
		parts = append(parts, branch+"["+strings.Join(names, "+")+"]")
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "any build"
	}
	return strings.Join(parts, ", ")
}

// apply changes the artifact-source triggers of one definition.
func apply(raw obj, action string, aliases, branches, tags []string) ([]string, error) {
	have := buildAliases(raw)
	targets := have
	if len(aliases) > 0 {
		targets = nil
		for _, want := range aliases {
			found := ""
			for _, h := range have {
				if strings.EqualFold(h, want) {
					found = h
				}
			}
			if found == "" {
				return nil, fmt.Errorf("no Build artifact with alias %q", want)
			}
			targets = append(targets, found)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no Build artifacts")
	}
	triggers, _ := raw["triggers"].([]interface{})
	var lines []string
	for _, alias := range targets {
		idx := -1
		for i, item := range triggers {
			t, _ := item.(obj)
			if a, _ := t["artifactAlias"].(string); strings.EqualFold(a, alias) {
				if tt, _ := t["triggerType"].(string); tt == "" || strings.EqualFold(tt, "artifactSource") || tt == "1" {
					idx = i
				}
			}
		}
		switch action {
		case "disable":
			if idx >= 0 {
				triggers = append(triggers[:idx:idx], triggers[idx+1:]...)
				lines = append(lines, fmt.Sprintf("[%s] disable continuous deployment", alias))
			}
		case "enable":
			if idx < 0 {
				t := obj{"triggerType": "artifactSource", "artifactAlias": alias, "triggerConditions": conditions(branches, tags)}
				triggers = append(triggers, t)
				lines = append(lines, fmt.Sprintf("[%s] enable continuous deployment (%s)", alias, filterKey(t)))
			} else if len(branches)+len(tags) > 0 {
				if line, changed := setFilters(triggers[idx].(obj), alias, branches, tags); changed {
					lines = append(lines, line)
				}
			}
		case "set-filters":
			if idx < 0 {
				continue
			}
			if line, changed := setFilters(triggers[idx].(obj), alias, branches, tags); changed {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	raw["triggers"] = triggers
	if triggers == nil {
		raw["triggers"] = []interface{}{}
	}
	return lines, nil
}

func setFilters(trigger obj, alias string, branches, tags []string) (string, bool) {
	before := filterKey(trigger)
	next := conditions(branches, tags)
	candidate := obj{"triggerConditions": next}
	if before == filterKey(candidate) {
		return "", false
	}
	trigger["triggerConditions"] = next
	return fmt.Sprintf("[%s] filters: %s -> %s", alias, before, filterKey(trigger)), true
}
