// Command redeploy-release-stages redeploys a named stage across existing releases in bulk.
package redeployreleasestages

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type job struct {
	pipeline string
	release  azuredevops.Release
	env      azuredevops.ReleaseEnvironmentStatus
}

const usageEpilog = `
Examples:
  redeploy-release-stages 'Example.Project\TEST' --stage Production --dry-run
  redeploy-release-stages 'Example.Project\TEST' --stage Production -y
  redeploy-release-stages 'Example.Project\TEST' --stage QA --all-releases --top 10 -y
Selection:
  By default, the named stage is redeployed from the newest eligible release in each pipeline.
  Eligible stages are completed: succeeded, rejected, partiallySucceeded, or canceled. Active and
  not-started stages are never selected. --all-releases selects every eligible release among the
  latest --top releases.
`

func Main() {
	fs := flag.NewFlagSet("redeploy-release-stages", flag.ExitOnError)
	stage := fs.String("stage", "", "Stage name to redeploy (required)")
	top := fs.Int("top", 20, "How many recent releases per pipeline to inspect")
	allReleases := fs.Bool("all-releases", false, "Redeploy the stage in every matching release, not only the newest")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	comment := fs.String("comment", "Redeployed by azure-devops-batch-operator", "Deployment comment")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists deployments without starting them")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Redeploys a named stage across existing releases under a path.")
		fmt.Fprintln(os.Stderr, "\nUsage: redeploy-release-stages <target> --stage <name> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"stage": true, "top": true, "filter": true, "comment": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || strings.TrimSpace(*stage) == "" || *top < 1 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *stage, *comment, *top, *allReleases, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func eligible(status string) bool {
	switch strings.ToLower(status) {
	case "succeeded", "rejected", "partiallysucceeded", "canceled":
		return true
	default:
		return false
	}
}

func selectJobs(pipeline string, releases []azuredevops.Release, stage string, allReleases bool) []job {
	var out []job
	for _, release := range releases {
		for _, env := range release.Environments {
			if !strings.EqualFold(env.Name, stage) || !eligible(env.Status) {
				continue
			}
			out = append(out, job{pipeline: pipeline, release: release, env: env})
			if !allReleases {
				return out
			}
		}
	}
	return out
}

func run(target, filter, stage, comment string, top int, allReleases bool, level string, dryRun, autoYes bool) error {
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
	var jobs []job
	for _, def := range defs {
		releases, err := cfg.ListReleases(project, def.ID, top)
		if err != nil {
			return fmt.Errorf("%s: %w", def.Name, err)
		}
		jobs = append(jobs, selectJobs(def.Name, releases, stage, allReleases)...)
	}
	fmt.Printf("Target: %s\nPipelines found: %d\nStage: %s\n", target, len(defs), stage)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no deployments will be started) <<<")
	}
	if len(jobs) == 0 {
		fmt.Println("\nNo eligible completed stages found.")
		return nil
	}
	fmt.Println("\n=== STAGES TO REDEPLOY ===")
	for _, item := range jobs {
		fmt.Printf("  %s / %s / %s (release=%d environment=%d, previous=%s)\n", item.pipeline, item.release.Name, item.env.Name, item.release.ID, item.env.ID, item.env.Status)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was redeployed.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nRedeploy %d stage(s)? (y/N): ", len(jobs))) {
		fmt.Println("Cancelled.")
		return nil
	}
	ok, failed := 0, 0
	for _, item := range jobs {
		if err := cfg.StartReleaseEnvironment(project, item.release.ID, item.env.ID, comment); err != nil {
			fmt.Printf("  x %s / %s / %s ERROR: %v\n", item.pipeline, item.release.Name, item.env.Name, err)
			failed++
		} else {
			fmt.Printf("  ok %s / %s / %s redeployment queued\n", item.pipeline, item.release.Name, item.env.Name)
			ok++
		}
	}
	fmt.Printf("\n=== DONE ===\nRedeployed: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d stage redeployment(s) failed", failed)
	}
	return nil
}
