// Command detect-pipeline-drift compares every release pipeline under a folder against a
// reference pipeline, a saved baseline, or the folder's own consensus, and groups the differences
// into recurring patterns.
package detectpipelinedrift

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
	"github.com/omerkaratas/azure-devops-go-automations/internal/drift"
	"github.com/omerkaratas/azure-devops-go-automations/internal/inventory"
)

const usageEpilog = `
Examples:
  detect-pipeline-drift 'Example.Project\TEST'
  detect-pipeline-drift 'Example.Project\TEST' --reference 'Example.Project\GOLDEN\Deploy'
  detect-pipeline-drift 'Example.Project\TEST' --write-baseline baseline.json
  detect-pipeline-drift 'Example.Project\TEST' --baseline baseline.json --fail-on-drift
Arguments:
  target            Required. Folder whose pipelines are checked.
  --reference       Compare against this pipeline instead of the folder consensus.
  --baseline        Compare against a baseline file (as written by --write-baseline; edit it to
                    set your own standard).
  --write-baseline  Save the baseline that was used to this file.
  --min-agreement   With the consensus baseline: percent of pipelines that must agree on a
                    setting for it to count as the norm (default 60). Others are listed as
                    having no consensus and are not judged.
  --format          text (default) or json.
  --include-values  Also compare non-secret variable values (names and flags only by default).
  --fail-on-drift   Exit with status 1 if any pipeline drifts.
  --filter          Only pipelines whose name contains this text.
  --level           PAT level (default: read).
Compared settings: variables, variable groups, artifacts, triggers, schedules, stages, approvals,
retention, jobs, pools, demands, timeouts and tasks. Names, ids and revisions are never compared.
`

type baselineFile struct {
	Source   string            `json:"source"`
	Settings map[string]string `json:"settings"`
	// NoConsensus lists settings the consensus could not agree on; they stay unjudged.
	NoConsensus []string `json:"noConsensus,omitempty"`
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("detect-pipeline-drift", flag.ExitOnError)
	ref := fs.String("reference", "", "Reference pipeline path")
	basePath := fs.String("baseline", "", "Baseline file to compare against")
	writeBase := fs.String("write-baseline", "", "Write the baseline used to this file")
	minAgree := fs.Int("min-agreement", 60, "Percent of pipelines that must agree for a consensus setting")
	format := fs.String("format", "text", "Output format: text or json")
	values := fs.Bool("include-values", false, "Compare non-secret variable values")
	fail := fs.Bool("fail-on-drift", false, "Exit 1 if any pipeline drifts")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Detects configuration drift across the release pipelines under a folder.")
		fmt.Fprintln(os.Stderr, "\nUsage: detect-pipeline-drift <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"reference": true, "baseline": true, "write-baseline": true, "min-agreement": true, "format": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || (*ref != "" && *basePath != "") || *minAgree < 1 || *minAgree > 100 || (*format != "text" && *format != "json") {
		fs.Usage()
		os.Exit(2)
	}
	drifted, err := run(fs.Arg(0), *filter, *level, *ref, *basePath, *writeBase, *format, float64(*minAgree)/100, *values)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if drifted && *fail {
		os.Exit(1)
	}
}

func run(target, filter, level, ref, basePath, writeBase, format string, minAgree float64, values bool) (bool, error) {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return false, err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return false, err
	}
	if len(defs) == 0 {
		return false, fmt.Errorf("no matching release pipelines found under `%s`", target)
	}
	opts := inventory.Options{IncludeValues: values}
	recs, err := inventory.Collect(cfg, project, defs, opts)
	if err != nil {
		return false, err
	}

	var baseline map[string]string
	var skip []string
	source := ""
	switch {
	case ref != "":
		rp, path, name, err := azuredevops.ParsePipelinePath(ref)
		if err != nil {
			return false, err
		}
		def, err := cfg.FindDefinition(rp, path, name)
		if err != nil {
			return false, fmt.Errorf("reference: %w", err)
		}
		raw, err := cfg.GetDefinitionDetailRaw(rp, def.ID)
		if err != nil {
			return false, err
		}
		refRec := inventory.Build(cfg, rp, raw, opts)
		baseline, source = refRec.Flatten(), "reference "+refRec.Label()
		// The reference itself is not a candidate for drift.
		kept := recs[:0]
		for _, r := range recs {
			if r.Label() != refRec.Label() {
				kept = append(kept, r)
			}
		}
		recs = kept
	case basePath != "":
		data, err := os.ReadFile(basePath)
		if err != nil {
			return false, err
		}
		var bf baselineFile
		if err := json.Unmarshal(data, &bf); err != nil || bf.Settings == nil {
			return false, fmt.Errorf("%s: not a baseline file (expected {\"settings\": {...}})", basePath)
		}
		baseline, skip, source = bf.Settings, bf.NoConsensus, "baseline file "+basePath
	}

	all := make(map[string]map[string]string, len(recs))
	for _, r := range recs {
		all[r.Label()] = r.Flatten()
	}
	if len(all) == 0 {
		return false, fmt.Errorf("nothing to compare")
	}
	if baseline == nil {
		if len(all) < 3 {
			return false, fmt.Errorf("consensus needs at least 3 pipelines (found %d); use --reference or --baseline", len(all))
		}
		baseline, skip = drift.Consensus(all, minAgree)
		source = fmt.Sprintf("consensus of %d pipelines (>= %d%% agreement)", len(all), int(minAgree*100))
	}
	if writeBase != "" {
		data, _ := json.MarshalIndent(baselineFile{Source: source, Settings: baseline, NoConsensus: skip}, "", "  ")
		if err := os.WriteFile(writeBase, data, 0o644); err != nil {
			return false, err
		}
		fmt.Fprintf(os.Stderr, "Baseline written to %s\n", writeBase)
	}

	res := drift.Analyze(baseline, all, skip)
	if format == "json" {
		out := struct {
			Target   string `json:"target"`
			Baseline string `json:"baseline"`
			drift.Result
		}{target, source, res}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return len(res.PerPipe) > 0, enc.Encode(out)
	}
	printText(target, source, res)
	return len(res.PerPipe) > 0, nil
}

func printText(target, source string, res drift.Result) {
	fmt.Printf("Target: %s\nBaseline: %s\nCompared: %d | Conforming: %d | Drifting: %d\n",
		target, source, res.Compared, len(res.Conforming), len(res.PerPipe))
	if len(res.NoNorm) > 0 {
		fmt.Printf("Settings with no consensus (not judged): %d\n", len(res.NoNorm))
	}
	if len(res.PerPipe) == 0 {
		fmt.Println("\nNo drift detected.")
		return
	}

	fmt.Println("\n=== RECURRING DRIFT PATTERNS (most common first) ===")
	for _, p := range res.Patterns {
		fmt.Printf("\n[%d pipeline(s)] %s\n  expected: %s\n  actual  : %s\n", len(p.Pipelines), p.Key, p.Expected, p.Actual)
		fmt.Printf("  in: %s\n", list(p.Pipelines, 5))
	}

	fmt.Println("\n=== PIPELINES THAT DRIFT IDENTICALLY ===")
	for i, c := range res.Clusters {
		fmt.Printf("\nGroup %d: %d pipeline(s), %d difference(s)\n  %s\n", i+1, len(c.Pipelines), len(c.Diffs), list(c.Pipelines, 8))
	}

	fmt.Println("\n=== DRIFT PER PIPELINE ===")
	names := make([]string, 0, len(res.PerPipe))
	for n := range res.PerPipe {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := len(res.PerPipe[names[i]]), len(res.PerPipe[names[j]])
		if a != b {
			return a > b
		}
		return names[i] < names[j]
	})
	for _, n := range names {
		fmt.Printf("%4d  %s\n", len(res.PerPipe[n]), n)
	}
}

func list(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:max], ", ") + fmt.Sprintf(", ... (+%d more)", len(items)-max)
}
