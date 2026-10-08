// Command promote-releases starts a target stage after a source stage succeeded.
package promotereleases

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type promotion struct {
	pipeline string
	release  azuredevops.Release
	target   azuredevops.ReleaseEnvironmentStatus
}

const usageEpilog = `
Examples:
  promote-releases 'Example.Project\TEST' --from Development --to Production --dry-run
  promote-releases 'Example.Project\TEST' --from QA --to Production -y
  promote-releases 'Example.Project\TEST' --from QA --to Production --all-releases --top 10 -y
Selection:
  By default, only the newest release of each pipeline is considered. Its source stage must be
  succeeded and its target stage must be notStarted. --all-releases considers every qualifying
  release among the latest --top releases.
`

func Main() {
	fs := flag.NewFlagSet("promote-releases", flag.ExitOnError)
	from := fs.String("from", "", "Source stage that must have succeeded (required)")
	to := fs.String("to", "", "Target stage to start (required)")
	top := fs.Int("top", 20, "How many recent releases per pipeline to inspect")
	allReleases := fs.Bool("all-releases", false, "Promote every qualifying release, not only the newest")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	comment := fs.String("comment", "Promoted by azure-devops-batch-operator", "Deployment comment")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists promotions without starting them")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Promotes existing releases from a succeeded source stage to a target stage.")
		fmt.Fprintln(os.Stderr, "\nUsage: promote-releases <target> --from <stage> --to <stage> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"from": true, "to": true, "top": true, "filter": true, "comment": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" || strings.EqualFold(*from, *to) || *top < 1 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *from, *to, *comment, *top, *allReleases, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func qualifies(release azuredevops.Release, from, to string) (azuredevops.ReleaseEnvironmentStatus, bool) {
	if strings.EqualFold(release.Status, "abandoned") || strings.EqualFold(release.Status, "draft") {
		return azuredevops.ReleaseEnvironmentStatus{}, false
	}
	sourceSucceeded := false
	var target azuredevops.ReleaseEnvironmentStatus
	targetFound := false
	for _, env := range release.Environments {
		if strings.EqualFold(env.Name, from) && strings.EqualFold(env.Status, "succeeded") {
			sourceSucceeded = true
		}
		if strings.EqualFold(env.Name, to) {
			target, targetFound = env, true
		}
	}
	return target, sourceSucceeded && targetFound && strings.EqualFold(target.Status, "notStarted")
}

func selectPromotions(pipeline string, releases []azuredevops.Release, from, to string, allReleases bool) []promotion {
	if !allReleases && len(releases) > 1 {
		releases = releases[:1]
	}
	var out []promotion
	for _, release := range releases {
		if target, ok := qualifies(release, from, to); ok {
			out = append(out, promotion{pipeline: pipeline, release: release, target: target})
		}
	}
	return out
}

func run(target, filter, from, to, comment string, top int, allReleases bool, level string, dryRun, autoYes bool) error {
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
	var jobs []promotion
	for _, def := range defs {
		releases, err := cfg.ListReleases(project, def.ID, top)
		if err != nil {
			return fmt.Errorf("%s: %w", def.Name, err)
		}
		jobs = append(jobs, selectPromotions(def.Name, releases, from, to, allReleases)...)
	}
	fmt.Printf("Target: %s\nPipelines found: %d\nPromotion: %s -> %s\n", target, len(defs), from, to)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no stages will be started) <<<")
	}
	if len(jobs) == 0 {
		fmt.Println("\nNo releases are ready for promotion.")
		return nil
	}
	fmt.Println("\n=== RELEASES TO PROMOTE ===")
	for _, job := range jobs {
		fmt.Printf("  %s / %s (release=%d) -> %s (environment=%d)\n", job.pipeline, job.release.Name, job.release.ID, job.target.Name, job.target.ID)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was promoted.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nPromote %d release(s)? (y/N): ", len(jobs))) {
		fmt.Println("Cancelled.")
		return nil
	}
	ok, failed := 0, 0
	for _, job := range jobs {
		if err := cfg.StartReleaseEnvironment(project, job.release.ID, job.target.ID, comment); err != nil {
			fmt.Printf("  x %s / %s / %s ERROR: %v\n", job.pipeline, job.release.Name, job.target.Name, err)
			failed++
		} else {
			fmt.Printf("  ok %s / %s promoted to %s\n", job.pipeline, job.release.Name, job.target.Name)
			ok++
		}
	}
	fmt.Printf("\n=== DONE ===\nPromoted: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d promotion(s) failed", failed)
	}
	return nil
}
