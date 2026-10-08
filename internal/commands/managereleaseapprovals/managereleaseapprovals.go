// Command manage-release-approvals approves or rejects pending classic-release approvals in bulk.
package managereleaseapprovals

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
  manage-release-approvals 'Example.Project\TEST' --action approve --dry-run
  manage-release-approvals 'Example.Project\TEST' --action approve --phase pre --stage Production -y
  manage-release-approvals 'Example.Project\TEST' --action reject --comment 'Change window closed' -y
Selection:
  The command inspects the latest --top-releases releases of every matching pipeline and changes
  only approvals that are currently pending. --phase accepts pre, post, or both.
This command acts on approvals of existing releases. update-pipeline-approvals changes the approval
configuration used by future releases.
`

func Main() {
	fs := flag.NewFlagSet("manage-release-approvals", flag.ExitOnError)
	action := fs.String("action", "", "Action to apply: approve or reject (required)")
	phase := fs.String("phase", "both", "Approval phase: pre, post, or both")
	stage := fs.String("stage", "", "Only approvals for this stage")
	topReleases := fs.Int("top-releases", 20, "Recent releases per pipeline to inspect")
	comment := fs.String("comment", "Updated by azure-devops-batch-operator", "Approval comment")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("manage"), "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists approvals without changing them")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Approves or rejects pending approvals across classic releases in bulk.")
		fmt.Fprintln(os.Stderr, "\nUsage: manage-release-approvals <target> --action approve|reject [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"action": true, "phase": true, "stage": true, "top-releases": true, "comment": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	*action = strings.ToLower(strings.TrimSpace(*action))
	*phase = strings.ToLower(strings.TrimSpace(*phase))
	if fs.NArg() != 1 || (*action != "approve" && *action != "reject") || (*phase != "pre" && *phase != "post" && *phase != "both") || *topReleases < 1 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *stage, *action, *phase, *comment, *topReleases, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func phaseMatches(approvalType, phase string) bool {
	if phase == "both" {
		return true
	}
	value := strings.ToLower(approvalType)
	if phase == "pre" {
		return value == "predeploy"
	}
	return value == "postdeploy"
}

func selectApprovals(values []azuredevops.ReleaseApproval, phase, stage string) []azuredevops.ReleaseApproval {
	var out []azuredevops.ReleaseApproval
	for _, approval := range values {
		if !strings.EqualFold(approval.Status, "pending") || !phaseMatches(approval.ApprovalType, phase) {
			continue
		}
		if stage != "" && !strings.EqualFold(stage, approval.ReleaseEnvironment.Name) {
			continue
		}
		out = append(out, approval)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReleaseDefinition.Name != out[j].ReleaseDefinition.Name {
			return out[i].ReleaseDefinition.Name < out[j].ReleaseDefinition.Name
		}
		if out[i].Release.ID != out[j].Release.ID {
			return out[i].Release.ID < out[j].Release.ID
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func run(target, filter, stage, action, phase, comment string, topReleases int, level string, dryRun, autoYes bool) error {
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
	var releaseIDs []int
	for _, def := range defs {
		releases, err := cfg.ListReleases(project, def.ID, topReleases)
		if err != nil {
			return fmt.Errorf("%s: %w", def.Name, err)
		}
		for _, release := range releases {
			releaseIDs = append(releaseIDs, release.ID)
		}
	}
	var approvals []azuredevops.ReleaseApproval
	// Keep releaseIdsFilter URLs bounded for large folders.
	for start := 0; start < len(releaseIDs); start += 50 {
		end := start + 50
		if end > len(releaseIDs) {
			end = len(releaseIDs)
		}
		values, err := cfg.ListPendingReleaseApprovals(project, releaseIDs[start:end], 1000)
		if err != nil {
			return err
		}
		approvals = append(approvals, values...)
	}
	approvals = selectApprovals(approvals, phase, stage)
	fmt.Printf("Target: %s\nPipelines found: %d\nAction: %s\n", target, len(defs), action)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no approvals will be changed) <<<")
	}
	if len(approvals) == 0 {
		fmt.Println("\nNo matching pending approvals found.")
		return nil
	}
	fmt.Println("\n=== APPROVALS TO UPDATE ===")
	for _, approval := range approvals {
		fmt.Printf("  %s / %s / %s [%s] approval=%d approver=%s\n",
			approval.ReleaseDefinition.Name, approval.Release.Name, approval.ReleaseEnvironment.Name,
			approval.ApprovalType, approval.ID, approval.Approver.DisplayName)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. No approvals were changed.")
		return nil
	}
	verb := "Approve"
	if action == "reject" {
		verb = "Reject"
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\n%s %d approval(s)? (y/N): ", verb, len(approvals))) {
		fmt.Println("Cancelled.")
		return nil
	}
	status := "approved"
	if action == "reject" {
		status = "rejected"
	}
	ok, failed := 0, 0
	for _, approval := range approvals {
		if err := cfg.UpdateReleaseApproval(project, approval.ID, status, comment); err != nil {
			fmt.Printf("  x approval %d (%s / %s) ERROR: %v\n", approval.ID, approval.Release.Name, approval.ReleaseEnvironment.Name, err)
			failed++
		} else {
			fmt.Printf("  ok approval %d %s (%s / %s)\n", approval.ID, status, approval.Release.Name, approval.ReleaseEnvironment.Name)
			ok++
		}
	}
	fmt.Printf("\n=== DONE ===\nUpdated: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d approval update(s) failed", failed)
	}
	return nil
}
