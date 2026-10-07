// Command update-pipeline-variable-groups links or unlinks variable groups in release pipelines.
package updatepipelinevariablegroups

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type intFlags []int

func (f *intFlags) String() string {
	parts := make([]string, len(*f))
	for i, n := range *f {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
func (f *intFlags) Set(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fmt.Errorf("expected a positive variable-group ID, got %q", value)
	}
	*f = append(*f, n)
	return nil
}

const usageEpilog = `
Examples:
  update-pipeline-variable-groups 'Example.Project\TEST' --link 12 --link 18 --dry-run
  update-pipeline-variable-groups 'Example.Project\TEST' --unlink 7 --scope stage --stage Production -y
Arguments:
  --link ID     Link a variable group by numeric ID; repeatable.
  --unlink ID   Unlink a variable group by numeric ID; repeatable.
  --scope       pipeline, stage, or all (default: pipeline).
  --stage       At stage/all scope, only update the named stage.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-variable-groups", flag.ExitOnError)
	links, unlinks := intFlags{}, intFlags{}
	fs.Var(&links, "link", "Variable-group ID to link; repeatable")
	fs.Var(&unlinks, "unlink", "Variable-group ID to unlink; repeatable")
	scope := fs.String("scope", "pipeline", "Association scope: pipeline, stage, or all")
	stage := fs.String("stage", "", "Only update this stage at stage/all scope")
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
		fmt.Fprintln(os.Stderr, "Links or unlinks variable groups across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-variable-groups <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"link": true, "unlink": true, "scope": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || len(links)+len(unlinks) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	if *scope != "pipeline" && *scope != "stage" && *scope != "all" {
		fmt.Fprintln(os.Stderr, "Error: --scope must be one of: pipeline, stage, all")
		os.Exit(2)
	}
	if *scope == "pipeline" && *stage != "" {
		fmt.Fprintln(os.Stderr, "Error: --stage requires --scope stage or all")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		return apply(raw, *scope, *stage, links, unlinks)
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Automated variable-group association update", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func apply(raw map[string]interface{}, scope, stage string, links, unlinks []int) []string {
	var changes []string
	if scope == "pipeline" || scope == "all" {
		if line := update(raw, "pipeline", links, unlinks); line != "" {
			changes = append(changes, line)
		}
	}
	if scope == "stage" || scope == "all" {
		batchupdate.EachEnvironment(raw, stage, func(name string, env map[string]interface{}) {
			if line := update(env, "stage "+name, links, unlinks); line != "" {
				changes = append(changes, line)
			}
		})
	}
	sort.Strings(changes)
	return changes
}

func update(owner map[string]interface{}, label string, links, unlinks []int) string {
	current := groupIDs(owner["variableGroups"])
	wanted := append([]int(nil), current...)
	for _, id := range links {
		if !has(wanted, id) {
			wanted = append(wanted, id)
		}
	}
	for _, id := range unlinks {
		wanted = remove(wanted, id)
	}
	if equal(current, wanted) {
		return ""
	}
	values := make([]interface{}, len(wanted))
	for i, id := range wanted {
		values[i] = id
	}
	owner["variableGroups"] = values
	return fmt.Sprintf("[%s] variable groups: %v -> %v", label, current, wanted)
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
		if n > 0 && !has(out, n) {
			out = append(out, n)
		}
	}
	return out
}
func has(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func remove(values []int, unwanted int) []int {
	out := values[:0]
	for _, value := range values {
		if value != unwanted {
			out = append(out, value)
		}
	}
	return out
}
func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
