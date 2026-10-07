// Command update-pipeline-artifacts replaces build artifact sources, branches, projects and aliases in release pipelines.
package updatepipelineartifacts

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type obj = map[string]interface{}

const usageEpilog = `
Examples:
  update-pipeline-artifacts 'Example.Project\TEST' --match-definition OldBuild --set-definition NewBuild --dry-run
  update-pipeline-artifacts 'Example.Project\TEST' --match-alias _OldBuild --set-alias _NewBuild --rewrite-references -y
  update-pipeline-artifacts 'Example.Project\TEST' --match-definition NewBuild --set-branch refs/heads/release -y
Matching (an artifact must satisfy every given matcher; at least one is required):
  --match-alias, --match-definition, --match-project, --match-branch   exact, case-insensitive
Changes (at least one is required):
  --set-definition  Point at another build pipeline, looked up by name in the (new) project.
  --set-project     Move the artifact to a build pipeline in another project (same name unless --set-definition).
  --set-branch      Default branch (e.g. refs/heads/main); the default version becomes "latest from branch".
  --set-alias       New artifact alias.
  --rewrite-references  With --set-alias, also rewrite $(Release.Artifacts.<alias>.*) and
                        $(System.DefaultWorkingDirectory)/<alias> in stages and steps.
Only Build artifacts are changed.
`

type options struct {
	matchAlias, matchDefinition, matchProject, matchBranch string
	setDefinition, setProject, setBranch, setAlias         string
	rewrite                                                bool
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-artifacts", flag.ExitOnError)
	var o options
	fs.StringVar(&o.matchAlias, "match-alias", "", "Only artifacts with this alias")
	fs.StringVar(&o.matchDefinition, "match-definition", "", "Only artifacts from this build pipeline name")
	fs.StringVar(&o.matchProject, "match-project", "", "Only artifacts from this project")
	fs.StringVar(&o.matchBranch, "match-branch", "", "Only artifacts with this default branch")
	fs.StringVar(&o.setDefinition, "set-definition", "", "New build pipeline name")
	fs.StringVar(&o.setProject, "set-project", "", "New project of the build pipeline")
	fs.StringVar(&o.setBranch, "set-branch", "", "New default branch")
	fs.StringVar(&o.setAlias, "set-alias", "", "New artifact alias")
	fs.BoolVar(&o.rewrite, "rewrite-references", false, "Rewrite alias references in stages and steps")
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
		fmt.Fprintln(os.Stderr, "Replaces build artifact sources, branches, projects or aliases across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-artifacts <target> [--match-*] [--set-*] [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"match-alias": true, "match-definition": true, "match-project": true, "match-branch": true, "set-definition": true, "set-project": true, "set-branch": true, "set-alias": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := o.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	resolver := &resolver{cache: map[string]azuredevops.BuildDefinitionRef{}}
	mutate := func(cfg azuredevops.Config, _ string, raw map[string]interface{}) []string {
		lines, err := apply(raw, o, func(project, name string) (azuredevops.BuildDefinitionRef, error) {
			return resolver.find(cfg, project, name)
		})
		if err != nil {
			name, _ := raw["name"].(string)
			fmt.Printf("  - skipped %s: %v\n", name, err)
			return nil
		}
		return lines
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Updated artifact sources", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func (o options) validate() error {
	if o.matchAlias == "" && o.matchDefinition == "" && o.matchProject == "" && o.matchBranch == "" {
		return fmt.Errorf("specify at least one --match-* option")
	}
	if o.setDefinition == "" && o.setProject == "" && o.setBranch == "" && o.setAlias == "" {
		return fmt.Errorf("specify at least one --set-* option")
	}
	if o.rewrite && o.setAlias == "" {
		return fmt.Errorf("--rewrite-references requires --set-alias")
	}
	return nil
}

type resolver struct {
	cache map[string]azuredevops.BuildDefinitionRef
}

func (r *resolver) find(cfg azuredevops.Config, project, name string) (azuredevops.BuildDefinitionRef, error) {
	key := strings.ToLower(project + "\x00" + name)
	if ref, ok := r.cache[key]; ok {
		return ref, nil
	}
	ref, err := cfg.FindBuildDefinition(project, name)
	if err == nil {
		r.cache[key] = ref
	}
	return ref, err
}

func str(m obj, keys ...string) string {
	var cur interface{} = m
	for _, k := range keys {
		next, _ := cur.(obj)
		cur = next[k]
	}
	s, _ := cur.(string)
	return s
}

func eq(a, b string) bool { return strings.EqualFold(a, b) }

func matches(a obj, o options) bool {
	ref, _ := a["definitionReference"].(obj)
	switch {
	case o.matchAlias != "" && !eq(str(a, "alias"), o.matchAlias):
		return false
	case o.matchDefinition != "" && !eq(str(ref, "definition", "name"), o.matchDefinition):
		return false
	case o.matchProject != "" && !eq(str(ref, "project", "name"), o.matchProject):
		return false
	case o.matchBranch != "" && !eq(str(ref, "defaultVersionBranch", "name"), o.matchBranch) && !eq(str(ref, "defaultVersionBranch", "id"), o.matchBranch):
		return false
	}
	return true
}

type finder func(project, name string) (azuredevops.BuildDefinitionRef, error)

// apply updates the matching Build artifacts of one definition.
func apply(raw obj, o options, find finder) ([]string, error) {
	artifacts, _ := raw["artifacts"].([]interface{})
	var lines []string
	type rename struct{ from, to string }
	var renames []rename
	for _, item := range artifacts {
		a, _ := item.(obj)
		if a == nil || !eq(str(a, "type"), "Build") || !matches(a, o) {
			continue
		}
		ref, _ := a["definitionReference"].(obj)
		if ref == nil {
			ref = obj{}
			a["definitionReference"] = ref
		}
		alias := str(a, "alias")
		var changes []string
		if o.setDefinition != "" || o.setProject != "" {
			project := firstNonEmpty(o.setProject, str(ref, "project", "name"))
			name := firstNonEmpty(o.setDefinition, str(ref, "definition", "name"))
			def, err := find(project, name)
			if err != nil {
				return nil, fmt.Errorf("artifact %q: %w", alias, err)
			}
			oldDef, _ := ref["definition"].(obj)
			if fmt.Sprint(oldDef["id"]) != fmt.Sprint(def.ID) || str(ref, "project", "id") != def.ProjectID {
				ref["definition"] = obj{"id": fmt.Sprint(def.ID), "name": def.Name}
				ref["project"] = obj{"id": def.ProjectID, "name": def.ProjectName}
				a["sourceId"] = def.ProjectID + ":" + fmt.Sprint(def.ID)
				changes = append(changes, fmt.Sprintf("source -> %s\\%s", def.ProjectName, def.Name))
			}
		}
		if o.setBranch != "" && !eq(str(ref, "defaultVersionBranch", "id"), o.setBranch) {
			ref["defaultVersionBranch"] = obj{"id": o.setBranch, "name": o.setBranch}
			ref["defaultVersionType"] = obj{"id": "latestWithBranchAndTagsType", "name": "Latest from a specific branch name and tags"}
			changes = append(changes, "branch -> "+o.setBranch)
		}
		if o.setAlias != "" && alias != o.setAlias {
			a["alias"] = o.setAlias
			renames = append(renames, rename{alias, o.setAlias})
			changes = append(changes, fmt.Sprintf("alias %s -> %s", alias, o.setAlias))
		}
		if len(changes) > 0 {
			lines = append(lines, fmt.Sprintf("[artifact %s] %s", alias, strings.Join(changes, ", ")))
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, item := range artifacts {
		a, _ := item.(obj)
		alias := strings.ToLower(str(a, "alias"))
		if seen[alias] {
			return nil, fmt.Errorf("alias %q would be used by more than one artifact", str(a, "alias"))
		}
		seen[alias] = true
	}
	if o.rewrite {
		for _, r := range renames {
			if n := rewriteReferences(raw["environments"], r.from, r.to); n > 0 {
				lines = append(lines, fmt.Sprintf("[references] rewrote %d value(s) from %s to %s", n, r.from, r.to))
			}
		}
	}
	return lines, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// rewriteReferences replaces alias references in every string under v and returns the number of strings changed.
func rewriteReferences(v interface{}, from, to string) int {
	quoted := regexp.QuoteMeta(from)
	artifacts := regexp.MustCompile(`(?i)(Release\.Artifacts\.)` + quoted + `(\.)`)
	paths := regexp.MustCompile(`(?i)(System\.DefaultWorkingDirectory\)[/\\])` + quoted + `([/\\"'\s]|$)`)
	count := 0
	var walk func(interface{}) interface{}
	walk = func(x interface{}) interface{} {
		switch t := x.(type) {
		case string:
			next := artifacts.ReplaceAllString(t, "${1}"+to+"${2}")
			next = paths.ReplaceAllString(next, "${1}"+to+"${2}")
			if next != t {
				count++
			}
			return next
		case []interface{}:
			for i := range t {
				t[i] = walk(t[i])
			}
		case obj:
			for k := range t {
				t[k] = walk(t[k])
			}
		}
		return x
	}
	walk(v)
	return count
}
