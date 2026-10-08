// Package app is the single entry point shared by the a22r binary and `go run ./cmd/tui`.
// With no arguments it starts the TUI, otherwise it runs the named command.
package app

import (
	"fmt"
	"os"
	"sort"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/auditpipelinepermissions"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/auditpipelinepolicy"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/backuppipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/cancelreleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/cleanupreleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/clonefolder"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/clonepipeline"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/comparefolders"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/comparepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/copypipelinestage"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/createfiles"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/createpowershellpipeline"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/deletepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/deletepipelinesteps"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/detectbrokenartifactreferences"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/detectdeprecatedtasks"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/detectpipelinedrift"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/detectpipelinevariableconflicts"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/enforcepipelinepolicy"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listbatchmanifests"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listpipelineagentjob"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listpipelineschedule"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listpipelinesteps"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listpipelinevariables"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listpoolmembers"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listpools"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listreleasehistory"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listreleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/listreleasestatus"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/managepipelinestages"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/managereleaseapprovals"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/orchestratereleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/promotereleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/redeployreleasestages"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/renameormovepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/replacepipelinecontent"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/replacepipelinevariablegroups"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/reportpipelineinventory"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/restorepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/resumebatch"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/retryfailedreleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/rollbackbatch"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/rollbackreleases"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/synchronizepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/triggerrelease"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineagentjob"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineapprovals"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineartifacts"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinecdtriggers"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinedemands"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinegates"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineretention"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineschedule"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinestagetriggers"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinevariablegroups"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinevariables"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/upgradepipelinetasks"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/validatereleasebatch"
	"github.com/omerkaratas/azure-devops-go-automations/internal/tui"
)

// Name is the installed binary name.
const Name = "a22r"

// version is set at release time: -ldflags "-X .../internal/app.version=v1.2.3"
var version = "dev"

var commands = map[string]func(){
	"audit-pipeline-permissions":         auditpipelinepermissions.Main,
	"orchestrate-releases":               orchestratereleases.Main,
	"promote-releases":                   promotereleases.Main,
	"audit-pipeline-policy":              auditpipelinepolicy.Main,
	"backup-pipelines":                   backuppipelines.Main,
	"detect-pipeline-drift":              detectpipelinedrift.Main,
	"report-pipeline-inventory":          reportpipelineinventory.Main,
	"restore-pipelines":                  restorepipelines.Main,
	"retry-failed-releases":              retryfailedreleases.Main,
	"resume-batch":                       resumebatch.Main,
	"rollback-batch":                     rollbackbatch.Main,
	"rollback-releases":                  rollbackreleases.Main,
	"list-batch-manifests":               listbatchmanifests.Main,
	"synchronize-pipelines":              synchronizepipelines.Main,
	"cancel-releases":                    cancelreleases.Main,
	"cleanup-releases":                   cleanupreleases.Main,
	"update-pipeline-retention":          updatepipelineretention.Main,
	"clone-pipeline":                     clonepipeline.Main,
	"clone-folder":                       clonefolder.Main,
	"compare-pipelines":                  comparepipelines.Main,
	"compare-folders":                    comparefolders.Main,
	"copy-pipeline-stage":                copypipelinestage.Main,
	"manage-pipeline-stages":             managepipelinestages.Main,
	"create-files":                       createfiles.Main,
	"create-powershell-pipeline":         createpowershellpipeline.Main,
	"delete-pipelines":                   deletepipelines.Main,
	"delete-pipeline-steps":              deletepipelinesteps.Main,
	"detect-broken-artifact-references":  detectbrokenartifactreferences.Main,
	"update-pipeline-cd-triggers":        updatepipelinecdtriggers.Main,
	"detect-deprecated-tasks":            detectdeprecatedtasks.Main,
	"upgrade-pipeline-tasks":             upgradepipelinetasks.Main,
	"validate-release-batch":             validatereleasebatch.Main,
	"detect-pipeline-variable-conflicts": detectpipelinevariableconflicts.Main,
	"enforce-pipeline-policy":            enforcepipelinepolicy.Main,
	"list-pool-members":                  listpoolmembers.Main,
	"list-pools":                         listpools.Main,
	"list-releases":                      listreleases.Main,
	"rename-or-move-pipelines":           renameormovepipelines.Main,
	"replace-pipeline-content":           replacepipelinecontent.Main,
	"list-pipeline-agent-job":            listpipelineagentjob.Main,
	"list-pipeline-schedule":             listpipelineschedule.Main,
	"list-pipeline-steps":                listpipelinesteps.Main,
	"list-pipeline-variables":            listpipelinevariables.Main,
	"list-release-history":               listreleasehistory.Main,
	"list-release-status":                listreleasestatus.Main,
	"manage-release-approvals":           managereleaseapprovals.Main,
	"redeploy-release-stages":            redeployreleasestages.Main,
	"trigger-release":                    triggerrelease.Main,
	"update-pipeline-agent-job":          updatepipelineagentjob.Main,
	"update-pipeline-approvals":          updatepipelineapprovals.Main,
	"update-pipeline-artifacts":          updatepipelineartifacts.Main,
	"update-pipeline-stage-triggers":     updatepipelinestagetriggers.Main,
	"update-pipeline-demands":            updatepipelinedemands.Main,
	"update-pipeline-gates":              updatepipelinegates.Main,
	"update-pipeline-schedule":           updatepipelineschedule.Main,
	"update-pipeline-variables":          updatepipelinevariables.Main,
	"update-pipeline-variable-groups":    updatepipelinevariablegroups.Main,
	"replace-pipeline-variable-groups":   replacepipelinevariablegroups.Main,
}

var commandDescriptions = map[string]string{
	"audit-pipeline-permissions":         "Audit release-pipeline permissions and flag broad or inconsistent access.",
	"audit-pipeline-policy":              "Check release pipelines against a reusable YAML policy without changing them.",
	"backup-pipelines":                   "Save matching release-pipeline definitions as local JSON backups.",
	"cancel-releases":                    "Cancel queued or in-progress deployments across matching releases.",
	"cleanup-releases":                   "Find and optionally delete old releases using status and retention rules.",
	"clone-folder":                       "Copy a release folder tree and all pipelines beneath it.",
	"clone-pipeline":                     "Copy one classic release pipeline to a new name or folder.",
	"compare-folders":                    "Compare pipeline names and configuration across two release folders.",
	"compare-pipelines":                  "Compare variables, stages, jobs, and tasks between two pipelines.",
	"copy-pipeline-stage":                "Copy a stage from one release pipeline into matching destination pipelines.",
	"create-files":                       "Create empty local files and directories from a list of paths.",
	"create-powershell-pipeline":         "Create a classic release pipeline from local PowerShell scripts.",
	"delete-pipeline-steps":              "Delete exactly named tasks from matching release pipelines.",
	"delete-pipelines":                   "Permanently delete matching classic release-pipeline definitions.",
	"detect-broken-artifact-references":  "Find missing build, repository, branch, and service-connection references.",
	"detect-deprecated-tasks":            "Find deprecated, disabled, missing, or unsupported pipeline tasks.",
	"detect-pipeline-drift":              "Detect configuration drift against a reference, baseline, or folder consensus.",
	"detect-pipeline-variable-conflicts": "Find inconsistent variable values and secret settings across pipelines.",
	"enforce-pipeline-policy":            "Update matching pipelines to comply with a reusable YAML policy.",
	"list-batch-manifests":               "List and inspect saved batch-operation manifests.",
	"list-pipeline-agent-job":            "Show agent pools, demands, conditions, and timeouts for pipeline jobs.",
	"list-pipeline-schedule":             "Show scheduled triggers configured on a release pipeline.",
	"list-pipeline-steps":                "Show stages, jobs, tasks, and scripts in a release pipeline.",
	"list-pipeline-variables":            "Show pipeline-level and stage-level variables.",
	"list-pool-members":                  "List agents belonging to matching Azure DevOps pools.",
	"list-pools":                         "List Azure DevOps agent pools and their agents.",
	"list-release-history":               "Show recent releases, creators, dates, and stage statuses for one pipeline.",
	"list-release-status":                "Summarize the latest release and stage status across matching pipelines.",
	"list-releases":                      "Browse classic release-pipeline folders and definitions.",
	"manage-pipeline-stages":             "Add, clone, rename, remove, or reorder stages across pipelines.",
	"manage-release-approvals":           "Approve or reject pending approvals across existing releases.",
	"orchestrate-releases":               "Create releases in controlled waves with concurrency, retries, and monitoring.",
	"promote-releases":                   "Promote existing releases after a source stage succeeds.",
	"redeploy-release-stages":            "Redeploy a completed stage across existing releases in bulk.",
	"rename-or-move-pipelines":           "Rename matching pipelines or move them to another release folder.",
	"replace-pipeline-content":           "Replace matching text in scripts, step titles, or pipeline variables.",
	"replace-pipeline-variable-groups":   "Replace one linked variable group with another across pipelines.",
	"report-pipeline-inventory":          "Export pipeline configuration inventory as text, JSON, or CSV.",
	"restore-pipelines":                  "Recreate or overwrite release pipelines from JSON backups.",
	"resume-batch":                       "Resume pending or failed definition updates from a saved manifest.",
	"retry-failed-releases":              "Retry failed stages from recent releases across matching pipelines.",
	"rollback-batch":                     "Restore pipeline definitions captured before a batch update.",
	"rollback-releases":                  "Create releases pinned to artifacts from the previous successful release.",
	"synchronize-pipelines":              "Copy selected components from a reference pipeline to matching pipelines.",
	"trigger-release":                    "Create releases for every matching classic release pipeline.",
	"update-pipeline-agent-job":          "Update agent pool, timeout, and related job settings across pipelines.",
	"update-pipeline-approvals":          "Configure pre-deployment and post-deployment approvers and policies.",
	"update-pipeline-artifacts":          "Change artifact projects, definitions, branches, aliases, or versions.",
	"update-pipeline-cd-triggers":        "Enable, disable, or filter continuous-deployment artifact triggers.",
	"update-pipeline-demands":            "Set, add, remove, or clear agent demands across pipeline jobs.",
	"update-pipeline-gates":              "Configure pre-deployment and post-deployment gates across stages.",
	"update-pipeline-retention":          "Standardize release and artifact retention settings across stages.",
	"update-pipeline-schedule":           "Create, update, pause, or remove scheduled release triggers.",
	"update-pipeline-stage-triggers":     "Configure stages to start automatically, sequentially, or manually.",
	"update-pipeline-variable-groups":    "Link or unlink shared variable groups across release pipelines.",
	"update-pipeline-variables":          "Set or remove pipeline and stage variables in bulk.",
	"upgrade-pipeline-tasks":             "Upgrade matching pipeline tasks to another supported major version.",
	"validate-release-batch":             "Validate stages, artifacts, queues, and active deployments before release.",
}

// Main dispatches on os.Args and never returns normally for a command (commands call os.Exit).
func Main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runTUI()
		return
	}

	switch args[0] {
	case "tui":
		runTUI()
		return
	case "version", "--version", "-v":
		fmt.Printf("%s %s\n", Name, version)
		return
	case "config-path":
		path, err := azuredevops.ConfigPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println(path)
		return
	case "help", "--help", "-h":
		usage(os.Stdout)
		return
	}

	run, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		os.Exit(2)
	}
	// Commands parse os.Args[1:] themselves.
	os.Args = append([]string{args[0]}, args[1:]...)
	run()
}

func runTUI() {
	if err := tui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func usage(w *os.File) {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)

	_, _ = fmt.Fprintf(w, "%s %s: batch operations for Azure DevOps release pipelines.\n\n", Name, version)
	_, _ = fmt.Fprintf(w, "Usage:\n  %s                    start the interactive UI\n", Name)
	_, _ = fmt.Fprintf(w, "  %s <command> [args]    run one command (see `%s <command> --help`)\n", Name, Name)
	_, _ = fmt.Fprintf(w, "  %s version             print the version\n\nCommands:\n", Name)
	for _, name := range names {
		_, _ = fmt.Fprintf(w, "  %-38s %s\n", name, commandDescriptions[name])
	}
	_, _ = fmt.Fprintf(w, "  %-38s %s\n", "config-path", "Print the active configuration file path.")
}
