// Command rollback-releases creates releases pinned to artifacts from a prior successful release.
package rollbackreleases

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

type rollbackJob struct {
	definition azuredevops.ReleaseDefinition
	current    azuredevops.Release
	baseline   azuredevops.Release
}

const usageEpilog = `
Examples:
  rollback-releases 'Example.Project\PROD' --dry-run
  rollback-releases 'Example.Project\PROD' --stage Production -y
Selection:
  For each pipeline, the newest release is treated as current and is never reused as the rollback
  baseline. The command finds the next older fully successful release and creates a new release
  pinned to that release's artifact IDs. A fully successful release has at least one succeeded stage,
  no failed or active stages, and may contain stages that never started.
  --stage may be repeated to start manual stages in the newly created rollback release.
`

func Main() {
	fs := flag.NewFlagSet("rollback-releases", flag.ExitOnError)
	top := fs.Int("top", 50, "How many recent releases per pipeline to inspect")
	stages := multiFlag{}
	fs.Var(&stages, "stage", "Manual stage to start in the rollback release; repeatable")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	description := fs.String("description", "Rollback created by azure-devops-batch-operator", "Description stored on each rollback release")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists rollback releases without creating them")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Creates releases using artifact versions from the previous successful release.")
		fmt.Fprintln(os.Stderr, "\nUsage: rollback-releases <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"top": true, "stage": true, "filter": true, "description": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *top < 2 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *description, []string(stages), *top, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func fullySuccessful(release azuredevops.Release) bool {
	if strings.EqualFold(release.Status, "abandoned") || strings.EqualFold(release.Status, "draft") {
		return false
	}
	succeeded := false
	for _, env := range release.Environments {
		switch strings.ToLower(env.Status) {
		case "succeeded":
			succeeded = true
		case "", "undefined", "notstarted":
			// Manual or later stages that never started do not invalidate the release.
		default:
			return false
		}
	}
	return succeeded
}

func previousSuccessful(releases []azuredevops.Release) (azuredevops.Release, bool) {
	if len(releases) < 2 {
		return azuredevops.Release{}, false
	}
	for _, release := range releases[1:] {
		if fullySuccessful(release) {
			return release, true
		}
	}
	return azuredevops.Release{}, false
}

func validArtifacts(artifacts []azuredevops.ReleaseArtifact) bool {
	if len(artifacts) == 0 {
		return false
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Alias) == "" || strings.TrimSpace(artifact.InstanceReference.ID) == "" {
			return false
		}
	}
	return true
}

func run(target, filter, description string, stages []string, top int, level string, dryRun, autoYes bool) error {
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
	var jobs []rollbackJob
	for _, def := range defs {
		releases, err := cfg.ListReleases(project, def.ID, top)
		if err != nil {
			return fmt.Errorf("%s: %w", def.Name, err)
		}
		baseline, ok := previousSuccessful(releases)
		if !ok {
			continue
		}
		baseline, err = cfg.GetRelease(project, baseline.ID)
		if err != nil {
			return fmt.Errorf("%s: read rollback release %d: %w", def.Name, baseline.ID, err)
		}
		if !validArtifacts(baseline.Artifacts) {
			return fmt.Errorf("%s: previous successful release %s has missing artifact version metadata", def.Name, baseline.Name)
		}
		jobs = append(jobs, rollbackJob{definition: def, current: releases[0], baseline: baseline})
	}
	fmt.Printf("Target: %s\nPipelines found: %d\n", target, len(defs))
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no rollback releases will be created) <<<")
	}
	if len(jobs) == 0 {
		fmt.Println("\nNo pipeline has a previous successful release to roll back to.")
		return nil
	}
	fmt.Println("\n=== ROLLBACK RELEASES TO CREATE ===")
	for _, job := range jobs {
		fmt.Printf("\n%s: current %s (id=%d) -> artifacts from %s (id=%d)\n", job.definition.Name, job.current.Name, job.current.ID, job.baseline.Name, job.baseline.ID)
		for _, artifact := range job.baseline.Artifacts {
			label := artifact.InstanceReference.Name
			if label == "" {
				label = artifact.InstanceReference.ID
			}
			fmt.Printf("  %-24s %s (id=%s)\n", artifact.Alias, label, artifact.InstanceReference.ID)
		}
	}
	if dryRun {
		fmt.Println("\nDry-run complete. No releases were created.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nCreate %d rollback release(s)? (y/N): ", len(jobs))) {
		fmt.Println("Cancelled.")
		return nil
	}
	ok, failed := 0, 0
	for _, job := range jobs {
		detail := fmt.Sprintf("%s; artifacts from %s (release id=%d)", description, job.baseline.Name, job.baseline.ID)
		created, err := cfg.CreateReleaseWithArtifacts(project, job.definition.ID, detail, stages, job.baseline.Artifacts)
		if err != nil {
			fmt.Printf("  x %s ERROR: %v\n", job.definition.Name, err)
			failed++
		} else {
			fmt.Printf("  ok %s -> %s (id=%d)\n", job.definition.Name, created.Name, created.ID)
			ok++
		}
	}
	fmt.Printf("\n=== DONE ===\nCreated: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d rollback release(s) failed", failed)
	}
	return nil
}
