// Command audit-pipeline-permissions reports who can view, edit, administer, trigger, approve and
// delete release pipelines, and flags overly broad grants and inconsistent folder permissions.
package auditpipelinepermissions

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
  audit-pipeline-permissions 'Example.Project\TEST'
  audit-pipeline-permissions 'Example.Project\TEST\CONFIG' --broad-groups 'Valid Users,Contributors' --fail-on-findings
Capabilities reported per user or group (effective: the nearest explicit setting wins, deny beats allow at the same level, inheritance cut where a folder or
pipeline does not inherit): view, edit, administer, trigger, approve, delete.
Findings:
  BROAD         A broad group (name contains one of --broad-groups; default "Valid Users,Everyone") can
                edit, administer, delete or manage approvers.
  INCONSISTENT  Pipelines in one folder have different effective permissions.
  NO-INHERIT    A folder or pipeline in scope does not inherit permissions from its parent.
Pipelines with identical effective permissions are grouped. Grants to groups are reported as granted
to the group; group members are not expanded. Reading permissions needs a token that may read
security information (for example the Security scope).
`

// Capability names, in report order.
var capabilities = []string{"view", "edit", "administer", "trigger", "approve", "delete"}

// capabilityOf maps a ReleaseManagement permission name to a capability ("" when not reported).
var capabilityOf = map[string]string{
	"viewreleasedefinition":        "view",
	"viewreleases":                 "view",
	"editreleasedefinition":        "edit",
	"editreleaseenvironment":       "edit",
	"managereleases":               "edit",
	"administerreleasepermissions": "administer",
	"createreleases":               "trigger",
	"managedeployments":            "trigger",
	"managereleaseapprovers":       "approve",
	"deletereleasedefinition":      "delete",
	"deletereleaseenvironment":     "delete",
	"deletereleases":               "delete",
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("audit-pipeline-permissions", flag.ExitOnError)
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	broad := fs.String("broad-groups", "Valid Users,Everyone", "Comma-separated name fragments that identify broad groups")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fail := fs.Bool("fail-on-findings", false, "Exit with status 1 when BROAD or INCONSISTENT findings exist")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Audits who can view, edit, administer, trigger, approve and delete release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: audit-pipeline-permissions <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"filter": true, "broad-groups": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, splitList(*broad), *level, *fail); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pipeline is a release pipeline to audit.
type pipeline struct {
	id     int
	name   string
	folder string // "\A\B" form, `\` for the root
}

// grant is the effective capability set of one identity.
type grant struct {
	descriptor string
	caps       map[string]bool
}

func run(target, filter string, broadGroups []string, level string, fail bool) error {
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
	projectID, err := cfg.GetProjectID(project)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	actions, err := cfg.SecurityActions(azuredevops.ReleaseManagementNamespace)
	if err != nil {
		return fmt.Errorf("read permission definitions: %w", err)
	}
	acls, err := cfg.ListACLs(azuredevops.ReleaseManagementNamespace, projectID, true)
	if err != nil {
		return fmt.Errorf("read permissions: %w", err)
	}
	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))

	pipelines := make([]pipeline, len(defs))
	for i, d := range defs {
		folder := d.Path
		if folder == "" {
			folder = `\`
		}
		pipelines[i] = pipeline{d.ID, d.Name, folder}
	}
	capsByBit := capabilityBits(actions)
	result := analyze(projectID, pipelines, acls, capsByBit)

	descriptors := result.descriptors()
	names, idErr := cfg.ResolveIdentities(descriptors)
	if idErr != nil {
		fmt.Printf("Note: some identities could not be resolved (%v); descriptors are shown instead.\n", idErr)
	}
	display := func(descriptor string) string {
		if id, ok := names[strings.ToLower(descriptor)]; ok && id.DisplayName != "" {
			return id.DisplayName
		}
		if i := strings.LastIndex(descriptor, ";"); i >= 0 {
			return descriptor[i+1:]
		}
		return descriptor
	}
	findings := report(result, display, broadGroups)
	fmt.Printf("\nAudit complete: %d pipeline(s), %d permission set(s), %d finding(s).\n", len(pipelines), len(result.groups), findings.total())
	if fail && findings.blocking() > 0 {
		return fmt.Errorf("found %d broad or inconsistent permission finding(s)", findings.blocking())
	}
	return nil
}

// capabilityBits maps each permission bit to its capability.
func capabilityBits(actions []azuredevops.SecurityAction) map[int]string {
	out := map[int]string{}
	for _, a := range actions {
		if c, ok := capabilityOf[strings.ToLower(a.Name)]; ok {
			out[a.Bit] = c
		}
	}
	return out
}

// chainTokens returns the security tokens from the project down to the pipeline.
func chainTokens(projectID string, p pipeline) []string {
	tokens := []string{projectID}
	cur := projectID
	for _, seg := range strings.Split(strings.Trim(p.folder, `\`), `\`) {
		if seg == "" {
			continue
		}
		cur += "/" + seg
		tokens = append(tokens, cur)
	}
	return append(tokens, fmt.Sprintf("%s/%d", cur, p.id))
}

// permissionSet is a canonical, comparable list of grants.
type permissionSet struct {
	signature string
	grants    []grant
	pipelines []pipeline
}

// analysis is the outcome of evaluating every pipeline.
type analysis struct {
	groups      []*permissionSet
	bySignature map[string]*permissionSet
	perPipeline map[int]string // pipeline id -> signature
	noInherit   []string       // folder / pipeline tokens (as readable paths) that do not inherit
	pipelines   []pipeline
}

func (a analysis) descriptors() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range a.groups {
		for _, gr := range g.grants {
			if !seen[gr.descriptor] {
				seen[gr.descriptor] = true
				out = append(out, gr.descriptor)
			}
		}
	}
	sort.Strings(out)
	return out
}

// analyze computes the effective capabilities of every identity for every pipeline.
func analyze(projectID string, pipelines []pipeline, acls []azuredevops.ACL, capsByBit map[int]string) analysis {
	byToken := map[string]azuredevops.ACL{}
	for _, acl := range acls {
		byToken[strings.ToLower(acl.Token)] = acl
	}
	res := analysis{bySignature: map[string]*permissionSet{}, perPipeline: map[int]string{}, pipelines: pipelines}
	seenNoInherit := map[string]bool{}
	for _, p := range pipelines {
		tokens := chainTokens(projectID, p)
		start := 0
		for i, t := range tokens {
			if acl, ok := byToken[strings.ToLower(t)]; ok && !acl.InheritPermissions && i > 0 {
				start = i
				if !seenNoInherit[t] {
					seenNoInherit[t] = true
					res.noInherit = append(res.noInherit, readablePath(projectID, t, p, i, len(tokens)))
				}
			}
		}
		// For every identity and permission bit the nearest explicit assignment decides, scanning from the
		// pipeline up to the first token that does not inherit; at one token deny beats allow.
		aces := map[string][]azuredevops.ACE{} // descriptor -> ACEs ordered from the project down
		for _, t := range tokens[start:] {
			acl, ok := byToken[strings.ToLower(t)]
			if !ok {
				continue
			}
			for key, ace := range acl.AcesDictionary {
				d := ace.Descriptor
				if d == "" {
					d = key
				}
				aces[d] = append(aces[d], ace)
			}
		}
		var grants []grant
		for d, list := range aces {
			caps := map[string]bool{}
			for bit, c := range capsByBit {
				for i := len(list) - 1; i >= 0; i-- {
					if (list[i].Allow|list[i].Deny)&bit == 0 {
						continue
					}
					if list[i].Deny&bit == 0 {
						caps[c] = true
					}
					break
				}
			}
			if len(caps) > 0 {
				grants = append(grants, grant{d, caps})
			}
		}
		sort.Slice(grants, func(i, j int) bool { return grants[i].descriptor < grants[j].descriptor })
		sig := signature(grants)
		set := res.bySignature[sig]
		if set == nil {
			set = &permissionSet{signature: sig, grants: grants}
			res.bySignature[sig] = set
			res.groups = append(res.groups, set)
		}
		set.pipelines = append(set.pipelines, p)
		res.perPipeline[p.id] = sig
	}
	sort.Strings(res.noInherit)
	return res
}

func readablePath(projectID, token string, p pipeline, depth, total int) string {
	if depth == total-1 {
		return fmt.Sprintf(`%s\%s (pipeline)`, strings.TrimRight(p.folder, `\`), p.name)
	}
	rest := strings.TrimPrefix(token, projectID)
	return strings.ReplaceAll(rest, "/", `\`) + " (folder)"
}

func capString(caps map[string]bool) string {
	var parts []string
	for _, c := range capabilities {
		if caps[c] {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, ", ")
}

func signature(grants []grant) string {
	var b strings.Builder
	for _, g := range grants {
		b.WriteString(g.descriptor + "=" + capString(g.caps) + ";")
	}
	return b.String()
}

type findingCounts struct{ broad, inconsistent, noInherit int }

func (f findingCounts) total() int    { return f.broad + f.inconsistent + f.noInherit }
func (f findingCounts) blocking() int { return f.broad + f.inconsistent }

var riskyCaps = []string{"edit", "administer", "delete", "approve"}

// report prints the permission sets and findings.
func report(a analysis, display func(string) string, broadGroups []string) findingCounts {
	var counts findingCounts
	for i, set := range a.groups {
		fmt.Printf("\n=== PERMISSION SET %d (%d pipeline(s)) ===\n", i+1, len(set.pipelines))
		for _, g := range set.grants {
			fmt.Printf("  %-45s %s\n", display(g.descriptor), capString(g.caps))
		}
		fmt.Println("  pipelines:")
		for j, p := range set.pipelines {
			if j == 10 {
				fmt.Printf("    ... and %d more\n", len(set.pipelines)-10)
				break
			}
			fmt.Printf("    %s\\%s\n", strings.TrimRight(p.folder, `\`), p.name)
		}
	}
	fmt.Println("\n=== FINDINGS ===")
	for i, set := range a.groups {
		for _, g := range set.grants {
			name := display(g.descriptor)
			if !matchesAny(name, broadGroups) {
				continue
			}
			var risky []string
			for _, c := range riskyCaps {
				if g.caps[c] {
					risky = append(risky, c)
				}
			}
			if len(risky) > 0 {
				counts.broad++
				fmt.Printf("  [BROAD] permission set %d: %q can %s (%d pipeline(s))\n", i+1, name, strings.Join(risky, ", "), len(set.pipelines))
			}
		}
	}
	for _, folder := range sortedFolders(a.pipelines) {
		sigs := map[string][]pipeline{}
		for _, p := range a.pipelines {
			if p.folder == folder {
				sigs[a.perPipeline[p.id]] = append(sigs[a.perPipeline[p.id]], p)
			}
		}
		if len(sigs) < 2 {
			continue
		}
		counts.inconsistent++
		fmt.Printf("  [INCONSISTENT] folder %s has %d different permission sets:\n", folder, len(sigs))
		var keys []string
		for k := range sigs {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return len(sigs[keys[i]]) > len(sigs[keys[j]]) })
		for _, k := range keys {
			var names []string
			for _, p := range sigs[k] {
				names = append(names, p.name)
			}
			fmt.Printf("      %d pipeline(s): %s\n", len(names), strings.Join(names, ", "))
		}
	}
	for _, path := range a.noInherit {
		counts.noInherit++
		fmt.Printf("  [NO-INHERIT] %s does not inherit permissions from its parent\n", path)
	}
	if counts.total() == 0 {
		fmt.Println("  none")
	}
	return counts
}

func sortedFolders(pipelines []pipeline) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range pipelines {
		if !seen[p.folder] {
			seen[p.folder] = true
			out = append(out, p.folder)
		}
	}
	sort.Strings(out)
	return out
}

func matchesAny(name string, fragments []string) bool {
	lower := strings.ToLower(name)
	for _, f := range fragments {
		if strings.Contains(lower, strings.ToLower(f)) {
			return true
		}
	}
	return false
}
