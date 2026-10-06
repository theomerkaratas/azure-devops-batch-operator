// Command compare-folders compares the release pipelines under two folders: how many pipelines
// each has, which pipeline names are shared or unique, and (for pipelines present in both) a
// content diff of their variables, stages, jobs and tasks.
package comparefolders

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
  compare-folders 'Example.Project\DEV\CONFIG' 'Example.Project\TEST\CONFIG'
  compare-folders 'Example.Project\DEV\CONFIG' 'Example.Project\TEST\CONFIG' --filter WCF
  compare-folders 'Example.Project\DEV\CONFIG' 'Example.Project\TEST\CONFIG' --names-only
Arguments:
  folder_a, folder_b  Required. Folder (or single pipeline) paths to compare, in
                      'Project\Folder\...' format.
  --filter            Optional. Only pipelines whose name contains this text (case-insensitive).
  --names-only        Only compare pipeline counts and names; skip the per-pipeline content diff.
  --level             Optional. PAT level (default: read). Choices: read | read-write | manage
                      Secret variable values are not returned by Azure DevOps and are not compared.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("compare-folders", flag.ExitOnError)
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	namesOnly := fs.Bool("names-only", false, "Only compare pipeline counts and names; skip the content diff")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Compares the release pipelines under two folders: counts, names, and content.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: compare-folders <folder_a> <folder_b> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 2 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), fs.Arg(1), *filter, *level, *namesOnly); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(folderA, folderB, filter, level string, namesOnly bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	projectA, defsA, err := batchupdate.SelectDefinitions(cfg, folderA, filter)
	if err != nil {
		return fmt.Errorf("%s: %w", folderA, err)
	}
	projectB, defsB, err := batchupdate.SelectDefinitions(cfg, folderB, filter)
	if err != nil {
		return fmt.Errorf("%s: %w", folderB, err)
	}

	byNameA := indexByName(defsA)
	byNameB := indexByName(defsB)

	fmt.Printf("A: %s (%d release(s))\nB: %s (%d release(s))\n", folderA, len(defsA), folderB, len(defsB))

	names := map[string]bool{}
	for n := range byNameA {
		names[n] = true
	}
	for n := range byNameB {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	var onlyA, onlyB, both []string
	for _, n := range sorted {
		_, inA := byNameA[n]
		_, inB := byNameB[n]
		switch {
		case inA && inB:
			both = append(both, n)
		case inA:
			onlyA = append(onlyA, n)
		default:
			onlyB = append(onlyB, n)
		}
	}

	fmt.Println("\n=== RELEASE NAMES ===")
	if len(onlyA) == 0 && len(onlyB) == 0 {
		fmt.Println("Same release names on both sides.")
	} else {
		for _, n := range onlyA {
			fmt.Printf("- only in A  %s\n", n)
		}
		for _, n := range onlyB {
			fmt.Printf("+ only in B  %s\n", n)
		}
	}
	fmt.Printf("\nOnly in A: %d | Only in B: %d | In both: %d\n", len(onlyA), len(onlyB), len(both))

	if namesOnly || len(both) == 0 {
		return nil
	}

	fmt.Println("\n=== CONTENT DIFF (releases present on both sides) ===")
	totalDiffs := 0
	changed := 0
	for _, n := range both {
		defA, defB := byNameA[n], byNameB[n]
		detailA, err := cfg.GetDefinitionDetail(projectA, defA.ID)
		if err != nil {
			return fmt.Errorf("%s\\%s: %w", folderA, n, err)
		}
		detailB, err := cfg.GetDefinitionDetail(projectB, defB.ID)
		if err != nil {
			return fmt.Errorf("%s\\%s: %w", folderB, n, err)
		}
		flatA := batchupdate.FlattenDetail(cfg, projectA, detailA)
		flatB := batchupdate.FlattenDetail(cfg, projectB, detailB)

		fmt.Printf("\n--- %s ---\n", n)
		diffs := batchupdate.DiffFlat("A", "B", flatA, flatB)
		if diffs == 0 {
			fmt.Println("No differences.")
		} else {
			changed++
		}
		totalDiffs += diffs
	}

	fmt.Printf("\n%d of %d shared release(s) differ (%d total difference(s)).\n", changed, len(both), totalDiffs)
	return nil
}

func indexByName(defs []azuredevops.ReleaseDefinition) map[string]azuredevops.ReleaseDefinition {
	out := make(map[string]azuredevops.ReleaseDefinition, len(defs))
	for _, d := range defs {
		out[d.Name] = d
	}
	return out
}
