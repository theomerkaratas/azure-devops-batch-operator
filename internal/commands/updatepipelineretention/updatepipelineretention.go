// Command update-pipeline-retention standardizes release retention policies on stages.
package updatepipelineretention

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type obj = map[string]interface{}

const usageEpilog = `
Examples:
  update-pipeline-retention 'Example.Project\TEST' --days 30 --releases 5 --retain-build=true --dry-run
  update-pipeline-retention 'Example.Project\TEST' --days 365 --stage Production,PreProd -y
Arguments:
  --days          Days to keep releases (at least 1).
  --releases      Minimum number of releases to keep (at least 1).
  --retain-build  true/false: keep the build artifacts of retained releases.
  --stage         Comma-separated stages to change (default: every stage).
Only the values you give are changed. The organization's maximum retention settings still apply:
Azure DevOps rejects or caps values above them.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-retention", flag.ExitOnError)
	days := fs.Int("days", 0, "Days to keep releases")
	releases := fs.Int("releases", 0, "Minimum releases to keep")
	retainBuild := fs.String("retain-build", "", "true or false: retain associated builds")
	stageList := fs.String("stage", "", "Comma-separated stages (default: all)")
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
		fmt.Fprintln(os.Stderr, "Standardizes release retention policies across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-retention <target> [--days N] [--releases N] [--retain-build bool] [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"days": true, "releases": true, "retain-build": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	p := policy{days: *days, releases: *releases}
	switch strings.ToLower(*retainBuild) {
	case "":
	case "true":
		t := true
		p.retainBuild = &t
	case "false":
		f := false
		p.retainBuild = &f
	default:
		fmt.Fprintln(os.Stderr, "Error: --retain-build must be true or false")
		os.Exit(2)
	}
	switch {
	case fs.NArg() != 1:
		fs.Usage()
		os.Exit(2)
	case p.days < 0 || p.releases < 0:
		fmt.Fprintln(os.Stderr, "Error: --days and --releases must be at least 1")
		os.Exit(2)
	case p.days == 0 && p.releases == 0 && p.retainBuild == nil:
		fmt.Fprintln(os.Stderr, "Error: specify at least one of --days, --releases, --retain-build")
		os.Exit(2)
	case !batchupdate.ValidWriteLevel(*level):
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	var stages []string
	for _, s := range strings.Split(*stageList, ",") {
		if s = strings.TrimSpace(s); s != "" {
			stages = append(stages, s)
		}
	}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		lines, err := apply(raw, p, stages)
		if err != nil {
			name, _ := raw["name"].(string)
			fmt.Printf("  - skipped %s: %v\n", name, err)
			return nil
		}
		return lines
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	if err := batchupdate.Run(opts, "Updated release retention policy", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

type policy struct {
	days, releases int
	retainBuild    *bool
}

func number(v interface{}) (int, bool) {
	f, ok := v.(float64)
	return int(f), ok
}

// apply sets the policy on the selected stages of one definition.
func apply(raw obj, p policy, selected []string) ([]string, error) {
	stages, _ := raw["environments"].([]interface{})
	found := map[string]bool{}
	var lines []string
	for _, item := range stages {
		stage, _ := item.(obj)
		name, _ := stage["name"].(string)
		if len(selected) > 0 {
			match := false
			for _, s := range selected {
				if strings.EqualFold(s, name) {
					match, found[strings.ToLower(s)] = true, true
				}
			}
			if !match {
				continue
			}
		}
		rp, _ := stage["retentionPolicy"].(obj)
		if rp == nil {
			rp = obj{}
		}
		var changes []string
		if cur, ok := number(rp["daysToKeep"]); p.days > 0 && (!ok || cur != p.days) {
			changes = append(changes, fmt.Sprintf("days %s -> %d", show(rp["daysToKeep"]), p.days))
			rp["daysToKeep"] = float64(p.days)
		}
		if cur, ok := number(rp["releasesToKeep"]); p.releases > 0 && (!ok || cur != p.releases) {
			changes = append(changes, fmt.Sprintf("releases %s -> %d", show(rp["releasesToKeep"]), p.releases))
			rp["releasesToKeep"] = float64(p.releases)
		}
		if cur, ok := rp["retainBuild"].(bool); p.retainBuild != nil && (!ok || cur != *p.retainBuild) {
			changes = append(changes, fmt.Sprintf("retain build %s -> %t", show(rp["retainBuild"]), *p.retainBuild))
			rp["retainBuild"] = *p.retainBuild
		}
		if len(changes) > 0 {
			stage["retentionPolicy"] = rp
			lines = append(lines, fmt.Sprintf("[%s] retention: %s", name, strings.Join(changes, ", ")))
		}
	}
	for _, s := range selected {
		if !found[strings.ToLower(s)] {
			return nil, fmt.Errorf("stage %q not found", s)
		}
	}
	return lines, nil
}

func show(v interface{}) string {
	if v == nil {
		return "unset"
	}
	if f, ok := v.(float64); ok {
		return fmt.Sprint(int(f))
	}
	return fmt.Sprint(v)
}
