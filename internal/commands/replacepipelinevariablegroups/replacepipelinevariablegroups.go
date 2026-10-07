// Command replace-pipeline-variable-groups replaces an old variable-group reference with a new
// one across release pipelines in a folder. Useful during migrations, environment separation, or
// variable-group restructuring, where every pipeline that links group A must now link group B.
package replacepipelinevariablegroups

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

type mapping struct {
	from, to int
}

type mappingFlags []mapping

func (f *mappingFlags) String() string {
	parts := make([]string, len(*f))
	for i, m := range *f {
		parts[i] = fmt.Sprintf("%d:%d", m.from, m.to)
	}
	return strings.Join(parts, ",")
}
func (f *mappingFlags) Set(value string) error {
	from, to, ok := strings.Cut(value, ":")
	fromID, err1 := strconv.Atoi(strings.TrimSpace(from))
	toID, err2 := strconv.Atoi(strings.TrimSpace(to))
	if !ok || err1 != nil || err2 != nil || fromID <= 0 || toID <= 0 {
		return fmt.Errorf("expected OLD_ID:NEW_ID, got %q", value)
	}
	*f = append(*f, mapping{from: fromID, to: toID})
	return nil
}

const usageEpilog = `
Examples:
  replace-pipeline-variable-groups 'Example.Project\TEST' --map 12:34 --dry-run
  replace-pipeline-variable-groups 'Example.Project\TEST' --map 12:34 --map 18:40 --scope all -y
Arguments:
  --map OLD:NEW  Replace variable-group ID OLD with ID NEW; repeatable.
  --scope        pipeline, stage, or all (default: all).
  --stage        At stage/all scope, only update the named stage.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("replace-pipeline-variable-groups", flag.ExitOnError)
	maps := mappingFlags{}
	fs.Var(&maps, "map", "OLD_ID:NEW_ID replacement; repeatable")
	scope := fs.String("scope", "all", "Association scope: pipeline, stage, or all")
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
		fmt.Fprintln(os.Stderr, "Replaces an old variable-group reference with a new one across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: replace-pipeline-variable-groups <target> --map OLD:NEW [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"map": true, "scope": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || len(maps) == 0 {
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
		return apply(raw, *scope, *stage, maps)
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Automated variable-group replacement", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func apply(raw map[string]interface{}, scope, stage string, maps []mapping) []string {
	var changes []string
	if scope == "pipeline" || scope == "all" {
		if line := replace(raw, "pipeline", maps); line != "" {
			changes = append(changes, line)
		}
	}
	if scope == "stage" || scope == "all" {
		batchupdate.EachEnvironment(raw, stage, func(name string, env map[string]interface{}) {
			if line := replace(env, "stage "+name, maps); line != "" {
				changes = append(changes, line)
			}
		})
	}
	sort.Strings(changes)
	return changes
}

// replace swaps any linked group ID found in maps for its replacement, in place, so the link
// order (which determines override precedence between variable groups) is preserved.
func replace(owner map[string]interface{}, label string, maps []mapping) string {
	current := groupIDs(owner["variableGroups"])
	wanted := append([]int(nil), current...)
	changed := false
	for i, id := range wanted {
		for _, m := range maps {
			if id == m.from {
				wanted[i] = m.to
				changed = true
				break
			}
		}
	}
	if !changed {
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
		if n > 0 {
			out = append(out, n)
		}
	}
	return out
}
