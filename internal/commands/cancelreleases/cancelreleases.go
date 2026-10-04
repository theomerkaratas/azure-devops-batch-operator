// Command cancel-releases cancels in-progress or queued stage deployments of many releases at once.
package cancelreleases

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  cancel-releases 'Example.Project\TEST\CONFIG' --dry-run
  cancel-releases 'Example.Project\TEST\CONFIG' --filter WCF --stage Development
  cancel-releases 'Example.Project\TEST\CONFIG' --abandon -y
Arguments:
  target     Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --top      Optional. How many recent releases per pipeline to inspect (default: 20).
  --stage    Optional. Only cancel deployments of the given stage name.
  --abandon  Optional. Also abandon each release that had a deployment canceled, so it can't
             be deployed any more.
  --filter   Optional. Only pipelines whose name contains this text (case-insensitive).
  --level    Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run  Lists what would be canceled without canceling anything.
  -y, --yes  Skips the confirmation prompt.
`

type job struct {
	pipeline string
	release  azuredevops.Release
	envs     []azuredevops.ReleaseEnvironmentStatus
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("cancel-releases", flag.ExitOnError)
	top := fs.Int("top", 20, "How many recent releases per pipeline to inspect")
	stage := fs.String("stage", "", "Only cancel deployments of the given stage name")
	abandon := fs.Bool("abandon", false, "Also abandon each release that had a deployment canceled")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists what would be canceled without canceling anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Cancels in-progress or queued deployments of releases under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: cancel-releases <target> [flags]")
		fmt.Fprintln(os.Stderr)
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
	if err := run(fs.Arg(0), *filter, *stage, *top, *abandon, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func isActive(status string) bool {
	s := strings.ToLower(status)
	return s == "inprogress" || s == "queued"
}
func run(target, filter, stage string, top int, abandon bool, level string, dryRun, autoYes bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under '%s'.\n", target)
		return nil
	}
	fmt.Printf("Target: %s\nPipelines found: %d\nLevel: %s\n", target, len(defs), level)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (nothing will be canceled) <<<")
	}
	fmt.Printf("\nInspecting the latest %d release(s) of each pipeline...\n", top)
	var jobs []job
	for _, d := range defs {
		releases, err := cfg.ListReleases(project, d.ID, top)
		if err != nil {
			return fmt.Errorf("%s: %w", d.Name, err)
		}
		for _, r := range releases {
			var envs []azuredevops.ReleaseEnvironmentStatus
			for _, e := range r.Environments {
				if isActive(e.Status) && (stage == "" || strings.EqualFold(e.Name, stage)) {
					envs = append(envs, e)
				}
			}
			if len(envs) > 0 {
				jobs = append(jobs, job{pipeline: d.Name, release: r, envs: envs})
			}
		}
	}
	if len(jobs) == 0 {
		fmt.Println("\nNo in-progress or queued deployments found.")
		return nil
	}
	fmt.Println("\n=== DEPLOYMENTS TO CANCEL ===")
	for _, j := range jobs {
		fmt.Printf("\n%s / %s (release id=%d)\n", j.pipeline, j.release.Name, j.release.ID)
		for _, e := range j.envs {
			fmt.Printf("  stage %-25s %s\n", e.Name, e.Status)
		}
		if abandon {
			fmt.Println("  + release will be abandoned")
		}
	}
	fmt.Printf("\n%d release(s) affected.\n", len(jobs))
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was canceled.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nCancel deployments in %d release(s)? (y/N): ", len(jobs))) {
		fmt.Println("Cancelled.")
		return nil
	}
	fmt.Println("\nCanceling...")
	ok, failed := 0, 0
	for _, j := range jobs {
		releaseOK := true
		for _, e := range j.envs {
			if err := cfg.CancelReleaseEnvironment(project, j.release.ID, e.ID); err != nil {
				fmt.Printf("  x %s / %s / %s ERROR: %v\n", j.pipeline, j.release.Name, e.Name, err)
				releaseOK = false
				continue
			}
			fmt.Printf("  ok %s / %s / %s canceled\n", j.pipeline, j.release.Name, e.Name)
		}
		if abandon && releaseOK {
			if err := cfg.AbandonRelease(project, j.release.ID); err != nil {
				fmt.Printf("  x %s / %s abandon ERROR: %v\n", j.pipeline, j.release.Name, err)
				releaseOK = false
			} else {
				fmt.Printf("  ok %s / %s abandoned\n", j.pipeline, j.release.Name)
			}
		}
		if releaseOK {
			ok++
		} else {
			failed++
		}
	}
	fmt.Printf("\n=== DONE ===\nSucceeded: %d | Failed: %d\n", ok, failed)
	return nil
}
