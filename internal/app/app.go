// Package app is the single entry point shared by the a22r binary and `go run ./cmd/tui`.
// With no arguments it starts the TUI, otherwise it runs the named command.
package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/auditpipelinepolicy"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/backuppipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/cancelreleases"
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
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/detectpipelinevariableconflicts"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/enforcepipelinepolicy"
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
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/renameormovepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/replacepipelinecontent"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/replacepipelinevariablegroups"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/restorepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/synchronizepipelines"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/triggerrelease"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineagentjob"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineapprovals"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineartifacts"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinecdtriggers"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinedemands"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinegates"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelineschedule"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinestagetriggers"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinevariablegroups"
	"github.com/omerkaratas/azure-devops-go-automations/internal/commands/updatepipelinevariables"
	"github.com/omerkaratas/azure-devops-go-automations/internal/tui"
)

// Name is the installed binary name.
const Name = "a22r"

// version is set at release time: -ldflags "-X .../internal/app.version=v1.2.3"
var version = "dev"

var commands = map[string]func(){
	"audit-pipeline-policy":              auditpipelinepolicy.Main,
	"backup-pipelines":                   backuppipelines.Main,
	"restore-pipelines":                  restorepipelines.Main,
	"synchronize-pipelines":              synchronizepipelines.Main,
	"cancel-releases":                    cancelreleases.Main,
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
	_, _ = fmt.Fprintf(w, "  %s version             print the version\n\nCommands:\n  %s\n", Name, strings.Join(names, "\n  "))
	_, _ = fmt.Fprintf(w, "  %s config-path         print the configuration file path\n", Name)
}
