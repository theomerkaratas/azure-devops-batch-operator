// Command detect-broken-artifact-references checks that the build pipelines, repositories, branches
// and service connections used by release pipelines still exist.
package detectbrokenartifactreferences

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

type obj = map[string]interface{}

const usageEpilog = `
Examples:
  detect-broken-artifact-references 'Example.Project\TEST'
  detect-broken-artifact-references 'Example.Project' --filter Deploy --fail-on-broken
Checks, per release pipeline:
  - Build artifacts: the build pipeline exists; its Azure Repos repository exists; the default branch exists.
  - Git artifacts: the repository exists and the default branch exists.
  - Service connections used by artifacts (GitHub, etc.) and by task inputs still exist in the project.
Findings are BROKEN (definitely missing) or UNCHECKED (could not be verified, e.g. no access or a
non-Azure-Repos repository). With --fail-on-broken the exit code is 1 when anything is BROKEN.
`

var guid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("detect-broken-artifact-references", flag.ExitOnError)
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	failOnBroken := fs.Bool("fail-on-broken", false, "Exit with status 1 when broken references are found")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Detects release pipelines whose artifact or service-connection references no longer exist.")
		fmt.Fprintln(os.Stderr, "\nUsage: detect-broken-artifact-references <target> [flags]")
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
	if err := run(fs.Arg(0), *filter, *level, *failOnBroken); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// finding is one problem with a pipeline reference.
type finding struct {
	broken bool
	text   string
}

// checker verifies references through the API; it is an interface so tests can fake it.
type checker interface {
	buildDefinition(project string, id int) (azuredevops.BuildDefinitionInfo, error)
	repository(project, id string) (bool, error)
	branch(project, repoID, branch string) (bool, error)
	endpoints(project string) (map[string]string, error)
}

type apiChecker struct{ cfg azuredevops.Config }

func (a apiChecker) buildDefinition(p string, id int) (azuredevops.BuildDefinitionInfo, error) {
	return a.cfg.GetBuildDefinitionInfo(p, id)
}
func (a apiChecker) repository(p, id string) (bool, error) { return a.cfg.GitRepositoryExists(p, id) }
func (a apiChecker) branch(p, r, b string) (bool, error)   { return a.cfg.GitBranchExists(p, r, b) }
func (a apiChecker) endpoints(p string) (map[string]string, error) {
	return a.cfg.ServiceEndpoints(p)
}

func run(target, filter, level string, failOnBroken bool) error {
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
	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))
	chk := apiChecker{cfg}
	affected, broken, unchecked := 0, 0, 0
	for _, d := range defs {
		raw, err := cfg.GetDefinitionDetailRaw(project, d.ID)
		if err != nil {
			return fmt.Errorf("read %s: %w", d.Name, err)
		}
		findings := inspect(raw, project, chk)
		if len(findings) == 0 {
			continue
		}
		affected++
		path := strings.TrimRight(d.Path, `\`)
		fmt.Printf("\n%s\\%s (id=%d)\n", path, d.Name, d.ID)
		for _, f := range findings {
			label := "UNCHECKED"
			if f.broken {
				label = "BROKEN"
				broken++
			} else {
				unchecked++
			}
			fmt.Printf("  [%s] %s\n", label, f.text)
		}
	}
	fmt.Printf("\nCheck complete: %d pipeline(s), %d with findings, %d broken reference(s), %d unchecked.\n", len(defs), affected, broken, unchecked)
	if failOnBroken && broken > 0 {
		return fmt.Errorf("found %d broken reference(s)", broken)
	}
	return nil
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

// inspect returns the findings for one release definition.
func inspect(raw obj, project string, chk checker) []finding {
	var out []finding
	broken := func(format string, args ...interface{}) {
		out = append(out, finding{true, fmt.Sprintf(format, args...)})
	}
	unchecked := func(format string, args ...interface{}) {
		out = append(out, finding{false, fmt.Sprintf(format, args...)})
	}
	connections := map[string][]string{} // lower-case GUID -> where used

	artifacts, _ := raw["artifacts"].([]interface{})
	for _, item := range artifacts {
		a, _ := item.(obj)
		if a == nil {
			continue
		}
		alias, typ := str(a, "alias"), str(a, "type")
		ref, _ := a["definitionReference"].(obj)
		artProject := firstNonEmpty(str(ref, "project", "id"), project)
		branch := firstNonEmpty(str(ref, "defaultVersionBranch", "id"), str(ref, "defaultVersionBranch", "name"))
		if id := str(ref, "connection", "id"); guid.MatchString(id) {
			connections[strings.ToLower(id)] = append(connections[strings.ToLower(id)], "artifact "+alias)
		}
		switch strings.ToLower(typ) {
		case "build":
			var id int
			fmt.Sscanf(str(ref, "definition", "id"), "%d", &id)
			if id == 0 {
				broken("artifact %s: no build pipeline is referenced", alias)
				continue
			}
			info, err := chk.buildDefinition(artProject, id)
			switch {
			case azuredevops.IsNotFound(err):
				broken("artifact %s: build pipeline %q (id=%d) no longer exists", alias, str(ref, "definition", "name"), id)
				continue
			case err != nil:
				unchecked("artifact %s: build pipeline id=%d could not be verified: %v", alias, id, err)
				continue
			}
			if !strings.EqualFold(info.RepoType, "TfsGit") {
				if branch != "" {
					unchecked("artifact %s: branch %s not verified (repository type %s)", alias, branch, firstNonEmpty(info.RepoType, "unknown"))
				}
				continue
			}
			out = append(out, checkRepoAndBranch(chk, artProject, alias, info.RepoID, branch)...)
		case "git":
			out = append(out, checkRepoAndBranch(chk, artProject, alias, str(ref, "definition", "id"), branch)...)
		case "github", "githubenterprise":
			if branch != "" {
				unchecked("artifact %s: GitHub branch %s not verified", alias, branch)
			}
		default:
			unchecked("artifact %s: artifact type %s is not checked", alias, typ)
		}
	}

	for _, stage := range stagesOf(raw) {
		collectEndpoints(stage["deployPhases"], str(stage, "name"), connections)
	}
	if len(connections) > 0 {
		endpoints, err := chk.endpoints(project)
		if err != nil {
			unchecked("service connections could not be listed: %v", err)
		} else {
			ids := make([]string, 0, len(connections))
			for id := range connections {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				if _, ok := endpoints[id]; !ok {
					where := dedupe(connections[id])
					broken("service connection %s not found in project (used by %s)", id, strings.Join(where, "; "))
				}
			}
		}
	}
	return out
}

func checkRepoAndBranch(chk checker, project, alias, repoID, branch string) []finding {
	if repoID == "" {
		return []finding{{true, fmt.Sprintf("artifact %s: no repository is referenced", alias)}}
	}
	exists, err := chk.repository(project, repoID)
	switch {
	case err != nil:
		return []finding{{false, fmt.Sprintf("artifact %s: repository %s could not be verified: %v", alias, repoID, err)}}
	case !exists:
		return []finding{{true, fmt.Sprintf("artifact %s: repository %s no longer exists", alias, repoID)}}
	}
	if branch == "" || strings.ContainsAny(branch, "*$") {
		return nil
	}
	ok, err := chk.branch(project, repoID, branch)
	switch {
	case err != nil:
		return []finding{{false, fmt.Sprintf("artifact %s: branch %s could not be verified: %v", alias, branch, err)}}
	case !ok:
		return []finding{{true, fmt.Sprintf("artifact %s: branch %s no longer exists", alias, branch)}}
	}
	return nil
}

func stagesOf(raw obj) []obj {
	var out []obj
	items, _ := raw["environments"].([]interface{})
	for _, item := range items {
		if m, _ := item.(obj); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// collectEndpoints gathers service-connection GUIDs used by task inputs.
func collectEndpoints(phases interface{}, stage string, into map[string][]string) {
	list, _ := phases.([]interface{})
	for _, p := range list {
		phase, _ := p.(obj)
		tasks, _ := phase["workflowTasks"].([]interface{})
		for _, t := range tasks {
			task, _ := t.(obj)
			inputs, _ := task["inputs"].(obj)
			for key, value := range inputs {
				lower := strings.ToLower(key)
				if !strings.Contains(lower, "connectedservice") && !strings.Contains(lower, "endpoint") && !strings.Contains(lower, "serviceconnection") && !strings.Contains(lower, "subscription") {
					continue
				}
				for _, id := range strings.Split(fmt.Sprint(value), ",") {
					if id = strings.TrimSpace(id); guid.MatchString(id) {
						id = strings.ToLower(id)
						into[id] = append(into[id], fmt.Sprintf("%s / %s", stage, firstNonEmpty(str(task, "name"), "task")))
					}
				}
			}
		}
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
