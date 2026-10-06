// Command create-powershell-pipeline creates a classic release pipeline whose agent job
// consists of inline PowerShell tasks.
package createpowershellpipeline

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const powerShellTaskID = "e213ff0f-5d5c-4791-802d-52ea3e7be1f1"

const usageEpilog = `
Examples:
  create-powershell-pipeline 'Example.Project\Deploy\Restart Services' --script stop.ps1 --script start.ps1 --pool WindowsAgents --dry-run
  create-powershell-pipeline 'Example.Project\Deploy\Health Check' --inline 'Invoke-WebRequest https://example.test/health' --queue-id 42 -y
Arguments:
  destination  Required. New pipeline path in 'Project\Folder\PipelineName' format.
  --script     PowerShell file to embed as a task. Repeat for multiple ordered tasks.
  --inline     Inline PowerShell to embed as a task. Repeat for multiple ordered tasks.
  --variable   Pipeline variable in NAME=VALUE form. Repeat to add multiple variables.
  --stage      Optional. Stage name (default: PowerShell).
  --pool       Agent pool/queue name. Use either --pool or --queue-id.
  --queue-id   Agent queue ID. Use either --queue-id or --pool.
  --pwsh       Run tasks with PowerShell Core instead of Windows PowerShell.
  --level      Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run    Validates and previews the pipeline without creating it.
  -y, --yes    Skips the confirmation prompt.
Notes:
  Script contents are embedded in the release definition; no build artifact is required.
  Tasks execute in the same agent job and in the order supplied.
`

type taskSource struct {
	kind  string
	value string
}

type multiValue []string

func (m *multiValue) String() string { return strings.Join(*m, ",") }
func (m *multiValue) Set(value string) error {
	*m = append(*m, value)
	return nil
}

type taskFlag struct {
	kind    string
	sources *[]taskSource
}

func (f taskFlag) String() string { return "" }
func (f taskFlag) Set(value string) error {
	*f.sources = append(*f.sources, taskSource{kind: f.kind, value: value})
	return nil
}

type scriptTask struct {
	name    string
	content string
}

type options struct {
	destination string
	sources     []taskSource
	stage       string
	pool        string
	queueID     int
	description string
	variables   []string
	pwsh        bool
	level       string
	dryRun      bool
	autoYes     bool
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("create-powershell-pipeline", flag.ExitOnError)
	var sources []taskSource
	var variables multiValue
	fs.Var(taskFlag{kind: "file", sources: &sources}, "script", "PowerShell file to embed as a task (repeatable)")
	fs.Var(taskFlag{kind: "inline", sources: &sources}, "inline", "Inline PowerShell task body (repeatable)")
	fs.Var(&variables, "variable", "Pipeline variable in NAME=VALUE form (repeatable)")
	stage := fs.String("stage", "PowerShell", "Stage name")
	pool := fs.String("pool", "", "Agent pool/queue name")
	queueID := fs.Int("queue-id", 0, "Agent queue ID")
	description := fs.String("description", "PowerShell release pipeline created by a22r", "Pipeline description")
	pwsh := fs.Bool("pwsh", false, "Use PowerShell Core")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level to use: read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Validates and previews without creating the pipeline")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Creates a classic release pipeline made of ordered PowerShell tasks.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: create-powershell-pipeline <destination> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{
		"script": true, "inline": true, "stage": true, "pool": true, "queue-id": true,
		"description": true, "variable": true, "level": true,
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || len(sources) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	if (*pool == "") == (*queueID == 0) {
		fmt.Fprintln(os.Stderr, "Error: specify exactly one of --pool or --queue-id")
		os.Exit(2)
	}
	if *queueID < 0 {
		fmt.Fprintln(os.Stderr, "Error: --queue-id must be greater than zero")
		os.Exit(2)
	}
	if strings.TrimSpace(*stage) == "" {
		fmt.Fprintln(os.Stderr, "Error: --stage cannot be empty")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	opts := options{
		destination: fs.Arg(0), sources: sources, stage: strings.TrimSpace(*stage),
		pool: strings.TrimSpace(*pool), queueID: *queueID, description: *description, variables: variables, pwsh: *pwsh,
		level: *level, dryRun: *dryRun, autoYes: *yes,
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	tasks, err := loadTasks(opts.sources)
	if err != nil {
		return err
	}
	variables, err := parseVariables(opts.variables)
	if err != nil {
		return err
	}
	project, path, name, err := azuredevops.ParsePipelinePath(opts.destination)
	if err != nil {
		return err
	}
	cfg, err := azuredevops.LoadConfig(opts.level)
	if err != nil {
		return err
	}
	if _, err := cfg.FindDefinition(project, path, name); err == nil {
		return fmt.Errorf("destination already exists: %s", opts.destination)
	} else if !strings.Contains(err.Error(), "pipeline not found:") {
		return err
	}
	queueID := opts.queueID
	if opts.pool != "" {
		queueID, err = cfg.ResolveQueueID(project, opts.pool)
		if err != nil {
			return err
		}
	}

	fmt.Printf("Destination: %s\nStage      : %s\nAgent queue: %d", opts.destination, opts.stage, queueID)
	if opts.pool != "" {
		fmt.Printf(" (%s)", opts.pool)
	}
	fmt.Printf("\nTasks      : %d\n", len(tasks))
	for i, task := range tasks {
		fmt.Printf("  %d. %s\n", i+1, task.name)
	}
	if len(variables) > 0 {
		fmt.Printf("Variables  : %d\n", len(variables))
	}
	if opts.dryRun {
		fmt.Println("\nDry-run complete. Nothing was created.")
		return nil
	}
	if !opts.autoYes {
		fmt.Print("\nCreate this release pipeline? (y/N): ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if path != `\` {
		if err := cfg.CreateFolder(project, path); err != nil {
			return fmt.Errorf("create folder %s: %w", path, err)
		}
	}
	definition := buildDefinition(name, path, opts.description, opts.stage, queueID, opts.pwsh, tasks, variables)
	created, err := cfg.CreateDefinitionRaw(project, definition, "Created by create-powershell-pipeline")
	if err != nil {
		return err
	}
	fmt.Printf("\nCreated %s (id=%v) with %d PowerShell task(s).\n", opts.destination, created["id"], len(tasks))
	return nil
}

func parseVariables(values []string) (map[string]interface{}, error) {
	variables := make(map[string]interface{}, len(values))
	for _, item := range values {
		name, value, ok := strings.Cut(item, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid variable %q; expected NAME=VALUE", item)
		}
		if _, exists := variables[name]; exists {
			return nil, fmt.Errorf("duplicate variable %q", name)
		}
		variables[name] = map[string]interface{}{
			"value": value, "isSecret": false, "allowOverride": false,
		}
	}
	return variables, nil
}

func loadTasks(sources []taskSource) ([]scriptTask, error) {
	tasks := make([]scriptTask, 0, len(sources))
	for _, source := range sources {
		switch source.kind {
		case "file":
			data, err := os.ReadFile(source.value)
			if err != nil {
				return nil, fmt.Errorf("read script %s: %w", source.value, err)
			}
			name := strings.TrimSuffix(filepath.Base(source.value), filepath.Ext(source.value))
			if name == "" {
				name = "PowerShell"
			}
			tasks = append(tasks, scriptTask{name: name, content: string(data)})
		case "inline":
			if strings.TrimSpace(source.value) == "" {
				return nil, fmt.Errorf("inline PowerShell task %d is empty", len(tasks)+1)
			}
			tasks = append(tasks, scriptTask{name: fmt.Sprintf("PowerShell %d", len(tasks)+1), content: source.value})
		default:
			return nil, fmt.Errorf("unsupported PowerShell task source %q", source.kind)
		}
	}
	return tasks, nil
}

func buildDefinition(name, path, description, stage string, queueID int, pwsh bool, scripts []scriptTask, variables map[string]interface{}) map[string]interface{} {
	workflowTasks := make([]map[string]interface{}, 0, len(scripts))
	for _, script := range scripts {
		workflowTasks = append(workflowTasks, map[string]interface{}{
			"taskId": powerShellTaskID, "version": "2.*", "name": script.name,
			"enabled": true, "alwaysRun": false, "continueOnError": false,
			"timeoutInMinutes": 0, "definitionType": "task",
			"inputs": map[string]interface{}{
				"targetType": "inline", "filePath": "", "arguments": "", "script": script.content,
				"errorActionPreference": "stop", "warningPreference": "default",
				"informationPreference": "default", "verbosePreference": "default",
				"debugPreference": "default", "progressPreference": "silentlyContinue",
				"failOnStderr": "false", "showWarnings": "true", "ignoreLASTEXITCODE": "false",
				"pwsh": fmt.Sprintf("%t", pwsh), "workingDirectory": "",
			},
		})
	}
	automatedApproval := func() map[string]interface{} {
		return map[string]interface{}{
			"approvals": []map[string]interface{}{{"rank": 1, "isAutomated": true, "isNotificationOn": false, "id": 0}},
			"approvalOptions": map[string]interface{}{
				"requiredApproverCount": nil, "releaseCreatorCanBeApprover": true,
				"autoTriggeredAndPreviousEnvironmentApprovedCanBeSkipped": false,
				"enforceIdentityRevalidation":                             false, "timeoutInMinutes": 0,
			},
		}
	}
	environment := map[string]interface{}{
		"id": 0, "name": stage, "rank": 1,
		"variables": map[string]interface{}{}, "variableGroups": []int{}, "schedules": []interface{}{},
		"conditions":         []map[string]interface{}{{"name": "ReleaseStarted", "conditionType": "event", "value": ""}},
		"preDeployApprovals": automatedApproval(), "postDeployApprovals": automatedApproval(),
		"retentionPolicy": map[string]interface{}{"daysToKeep": 30, "releasesToKeep": 3, "retainBuild": true},
		"environmentOptions": map[string]interface{}{
			"emailNotificationType": "OnlyOnFailure", "emailRecipients": "release.environment.owner;release.creator",
			"skipArtifactsDownload": true, "timeoutInMinutes": 0, "enableAccessToken": false,
			"publishDeploymentStatus": false, "badgeEnabled": false, "autoLinkWorkItems": false,
		},
		"deployPhases": []map[string]interface{}{{
			"rank": 1, "phaseType": "agentBasedDeployment", "name": "Run PowerShell", "workflowTasks": workflowTasks,
			"deploymentInput": map[string]interface{}{
				"parallelExecution":     map[string]interface{}{"parallelExecutionType": "none"},
				"skipArtifactsDownload": true, "timeoutInMinutes": 0, "jobCancelTimeoutInMinutes": 1,
				"queueId": queueID, "demands": []string{}, "enableAccessToken": false,
			},
		}},
	}
	return map[string]interface{}{
		"name": name, "path": path, "description": description, "source": "restApi",
		"releaseNameFormat": "Release-$(rev:r)", "artifacts": []interface{}{},
		"variables": variables, "variableGroups": []int{}, "triggers": []interface{}{},
		"tags": []string{}, "environments": []map[string]interface{}{environment},
	}
}
