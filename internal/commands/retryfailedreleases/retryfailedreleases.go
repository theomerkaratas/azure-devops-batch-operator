// Command retry-failed-releases redeploys failed stages across existing releases in bulk.
package retryfailedreleases

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type retryJob struct {
	pipeline string
	release  azuredevops.Release
	envs     []azuredevops.ReleaseEnvironmentStatus
}

const usageEpilog = `
Examples:
  retry-failed-releases 'Example.Project\TEST' --dry-run
  retry-failed-releases 'Example.Project\TEST' --stage Production -y
  retry-failed-releases 'Example.Project\TEST' --all-failed --include-canceled --top 50 -y
Selection:
  By default, only the newest release with a failed stage in each pipeline is selected. Failed
  means rejected or partiallySucceeded. --all-failed selects every matching release among the
  latest --top releases. --include-canceled also selects canceled stages.
`

func Main() {
	fs := flag.NewFlagSet("retry-failed-releases", flag.ExitOnError)
	top := fs.Int("top", 20, "How many recent releases per pipeline to inspect")
	stage := fs.String("stage", "", "Only retry this stage name")
	allFailed := fs.Bool("all-failed", false, "Retry every matching failed release, not only the newest per pipeline")
	includeCanceled := fs.Bool("include-canceled", false, "Treat canceled stages as retryable")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists deployments without retrying them")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Retries failed stages across releases under a path.")
		fmt.Fprintln(os.Stderr, "\nUsage: retry-failed-releases <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"top": true, "stage": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *top < 1 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *stage, *top, *allFailed, *includeCanceled, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func retryable(status string, includeCanceled bool) bool {
	switch strings.ToLower(status) {
	case "rejected", "partiallysucceeded":
		return true
	case "canceled":
		return includeCanceled
	default:
		return false
	}
}

func stageReached(status string) bool {
	switch strings.ToLower(status) {
	case "", "undefined", "notstarted":
		return false
	default:
		return true
	}
}

func selectJobs(pipeline string, releases []azuredevops.Release, stage string, allFailed, includeCanceled bool) []retryJob {
	var jobs []retryJob
	reached := map[string]bool{}
	for _, rel := range releases {
		var envs []azuredevops.ReleaseEnvironmentStatus
		for _, env := range rel.Environments {
			if stage != "" && !strings.EqualFold(stage, env.Name) {
				continue
			}
			key := strings.ToLower(env.Name)
			if retryable(env.Status, includeCanceled) && !reached[key] {
				envs = append(envs, env)
			}
			if stageReached(env.Status) {
				reached[key] = true
			}
		}
		if len(envs) == 0 {
			continue
		}
		jobs = append(jobs, retryJob{pipeline: pipeline, release: rel, envs: envs})
		if !allFailed {
			break
		}
	}
	return jobs
}

func run(target, filter, stage string, top int, allFailed, includeCanceled bool, level string, dryRun, autoYes bool) error {
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
	var jobs []retryJob
	for _, def := range defs {
		releases, err := cfg.ListReleases(project, def.ID, top)
		if err != nil {
			return fmt.Errorf("%s: %w", def.Name, err)
		}
		jobs = append(jobs, selectJobs(def.Name, releases, stage, allFailed, includeCanceled)...)
	}

	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no deployments will be retried) <<<")
	}
	if len(jobs) == 0 {
		fmt.Println("\nNo retryable failed deployments found.")
		return nil
	}
	fmt.Println("\n=== DEPLOYMENTS TO RETRY ===")
	total := 0
	for _, job := range jobs {
		fmt.Printf("\n%s / %s (release id=%d)\n", job.pipeline, job.release.Name, job.release.ID)
		for _, env := range job.envs {
			fmt.Printf("  stage %-25s %s\n", env.Name, env.Status)
			total++
		}
	}
	fmt.Printf("\n%d stage deployment(s) in %d release(s).\n", total, len(jobs))
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was retried.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nRetry %d stage deployment(s)? (y/N): ", total)) {
		fmt.Println("Cancelled.")
		return nil
	}

	ok, failed := 0, 0
	for _, job := range jobs {
		for _, env := range job.envs {
			if err := cfg.RetryReleaseEnvironment(project, job.release.ID, env.ID); err != nil {
				fmt.Printf("  x %s / %s / %s ERROR: %v\n", job.pipeline, job.release.Name, env.Name, err)
				failed++
			} else {
				fmt.Printf("  ok %s / %s / %s retry queued\n", job.pipeline, job.release.Name, env.Name)
				ok++
			}
		}
	}
	fmt.Printf("\n=== DONE ===\nRetried: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d stage deployment(s) could not be retried", failed)
	}
	return nil
}
