package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

// fieldKind distinguishes free-text input fields from fixed-choice fields.
type fieldKind int

const (
	fieldText fieldKind = iota
	fieldTextarea
	fieldChoice
)

// field is one input of a command's form. Text fields are backed by a textinput.Model;
// choice fields cycle through a fixed set of values with the left/right keys.
type field struct {
	key       string
	label     string
	help      string // short explanation of what this field is for, shown on the form screen
	kind      fieldKind
	required  bool
	choices   []string
	choiceIdx int
	input     textinput.Model
	area      textarea.Model
	pick      pickMode // non-zero: enter opens the path picker
}

// withHelp sets the field's short explanation and returns the field, so it can be chained
// onto the textField/choiceField/boolField/textareaField constructors.
func (f *field) withHelp(text string) *field {
	f.help = text
	return f
}

func textareaField(key, label, placeholder string, height int, required bool) *field {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.SetWidth(60)
	ta.SetHeight(height)
	ta.ShowLineNumbers = true
	return &field{key: key, label: label, kind: fieldTextarea, required: required, area: ta}
}

// pickModes marks which form field keys are Azure DevOps paths and what the picker accepts.
var pickModes = map[string]pickMode{
	"target":        pickAny,
	"pipeline_path": pickPipeline,
	"pipeline_a":    pickPipeline,
	"pipeline_b":    pickPipeline,
	"folder_a":      pickAny,
	"folder_b":      pickAny,
	"source":        pickPipeline,
	"destination":   pickNewPipeline,
	"move_to":       pickFolder,
}

func textField(key, label, placeholder string, required bool) *field {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Width = 60
	return &field{key: key, label: label, kind: fieldText, required: required, input: ti}
}

func choiceField(key, label string, choices []string, defaultIdx int) *field {
	if key == "level" {
		configured := azuredevops.DefaultLevel(choices[defaultIdx])
		for i, choice := range choices {
			if choice == configured {
				defaultIdx = i
				break
			}
		}
	}
	return &field{key: key, label: label, kind: fieldChoice, choices: choices, choiceIdx: defaultIdx}
}

func boolField(key, label string, defaultYes bool) *field {
	idx := 0
	if defaultYes {
		idx = 1
	}
	return choiceField(key, label, []string{"no", "yes"}, idx)
}

// value returns the field's current value as a plain string.
func (f *field) value() string {
	if f.kind == fieldChoice {
		return f.choices[f.choiceIdx]
	}
	if f.kind == fieldTextarea {
		return strings.TrimSpace(f.area.Value())
	}
	return strings.TrimSpace(f.input.Value())
}

// commandSpec describes one operation offered by the TUI: which package to run, which
// form fields to collect, and how to turn the collected values into CLI arguments.
type commandSpec struct {
	id          string
	description string
	newFields   func() []*field
	buildArgs   func(values map[string]string) ([]string, error)
}

const levelHelp = "Which personal access token to use: read-only tokens for read commands, read-write/manage for commands that change pipelines."
const dryRunHelp = "Yes: preview the changes without applying them. No: apply the changes for real."
const filterHelp = "Optional text that must appear in a pipeline's name; narrows down the target."

func readLevels() []string { return azuredevops.Levels() }

// splitList splits a ';'-separated form value into trimmed, non-empty items.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// writeTail appends the flags shared by all write commands: filter, level and dry-run/--yes.
// The TUI's own Run action is the confirmation, so non-dry runs skip the stdin prompt.
func writeTail(args []string, v map[string]string) []string {
	if v["filter"] != "" {
		args = append(args, "--filter", v["filter"])
	}
	args = append(args, "--level", v["level"])
	if v["dry_run"] == "yes" {
		return append(args, "--dry-run")
	}
	return append(args, "--yes")
}

func init() { commandSpecs = append(commandSpecs, writeCommandSpecs...) }

var writeCommandSpecs = []commandSpec{
	{
		id:          "create-powershell-pipeline",
		description: "Creates a classic release pipeline made of ordered PowerShell tasks.",
		newFields: func() []*field {
			return []*field{
				textField("destination", "New pipeline path", `e.g. Example.Project\Deploy\Restart Services`, true).
					withHelp("Path where the new pipeline will be created, as 'Project\\Folder\\PipelineName'."),
				textField("scripts", "PowerShell files (;-separated)", "optional: stop.ps1;start.ps1", false).
					withHelp("Local PowerShell files to embed as ordered tasks. List multiple files separated by ';'."),
				textareaField("inline", "Inline PowerShell", "Enter a PowerShell script directly here", 5, false).
					withHelp("A PowerShell script typed directly here, embedded as its own task. At least one of this or a file is required."),
				textareaField("variables", "Variables (NAME=VALUE per line)", "Environment=Production\nServiceName=Example", 4, false).
					withHelp("Optional pipeline variables, one NAME=VALUE pair per line."),
				textField("stage", "Stage name (optional)", "PowerShell", false).
					withHelp("Name of the single stage that holds the tasks. Defaults to 'PowerShell'."),
				textField("pool", "Agent pool name", "use this or queue ID", false).
					withHelp("Agent pool to run the tasks on, by name. Use this or the queue ID below, not both."),
				textField("queue_id", "Agent queue ID", "use this or pool name", false).
					withHelp("Agent pool to run the tasks on, by queue ID. Use this or the pool name above, not both."),
				textField("description", "Description (optional)", "PowerShell release pipeline created by a22r", false).
					withHelp("Description stored on the new pipeline definition."),
				boolField("pwsh", "Use PowerShell Core", false).
					withHelp("Yes: run tasks with PowerShell Core (pwsh). No: run with Windows PowerShell."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if len(splitList(v["scripts"])) == 0 && strings.TrimSpace(v["inline"]) == "" {
				return nil, fmt.Errorf("specify PowerShell files or an inline script")
			}
			if (v["pool"] == "") == (v["queue_id"] == "") {
				return nil, fmt.Errorf("specify either agent pool or queue ID")
			}
			args := []string{v["destination"]}
			for _, script := range splitList(v["scripts"]) {
				args = append(args, "--script", script)
			}
			if strings.TrimSpace(v["inline"]) != "" {
				args = append(args, "--inline", v["inline"])
			}
			for _, variable := range strings.Split(v["variables"], "\n") {
				if variable = strings.TrimSpace(variable); variable != "" {
					args = append(args, "--variable", variable)
				}
			}
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			if v["pool"] != "" {
				args = append(args, "--pool", v["pool"])
			} else {
				args = append(args, "--queue-id", v["queue_id"])
			}
			if v["description"] != "" {
				args = append(args, "--description", v["description"])
			}
			if v["pwsh"] == "yes" {
				args = append(args, "--pwsh")
			}
			args = append(args, "--level", v["level"])
			if v["dry_run"] == "yes" {
				return append(args, "--dry-run"), nil
			}
			return append(args, "--yes"), nil
		},
	},
	{
		id:          "delete-pipelines",
		description: "Permanently deletes matching classic release pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\ARCHIVE`, true).
					withHelp("Folder or single pipeline path to delete from."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				textField("comment", "Audit comment", "Deleted by azure-devops-batch-operator", false).
					withHelp("Comment recorded in Azure DevOps' audit log for this deletion."),
				boolField("force", "Force (cancel active deployments)", false).
					withHelp("Yes: cancel any active deployments first, then delete. No: fail if a pipeline has an active deployment."),
				choiceField("level", "PAT level", []string{"manage"}, 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--level", v["level"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			if v["comment"] != "" {
				args = append(args, "--comment", v["comment"])
			}
			if v["force"] == "yes" {
				args = append(args, "--force")
			}
			if v["dry_run"] == "yes" {
				return append(args, "--dry-run"), nil
			}
			return append(args, "--yes"), nil
		},
	},
	{
		id:          "list-pool-members",
		description: "Lists the agents that belong to matching agent pools.",
		newFields: func() []*field {
			return []*field{
				textField("pool", "Pool name (optional)", "empty = every pool; partial names are accepted", false).
					withHelp("Pool name to filter by (partial match). Leave empty to list every pool's agents."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			var args []string
			if v["pool"] != "" {
				args = append(args, v["pool"])
			}
			return append(args, "--level", v["level"]), nil
		},
	},
	{
		id:          "list-pools",
		description: "Lists agent pools, or the agents (members) of matching pools.",
		newFields: func() []*field {
			return []*field{
				textField("pool", "Pool name (optional)", "empty = all pools; text = list its members", false).
					withHelp("Pool name to filter by. Leave empty to list every pool."),
				boolField("members", "List members of all pools", false).
					withHelp("Yes: also list the agents (members) of every pool, not just the pool names."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			var args []string
			if v["pool"] != "" {
				args = append(args, v["pool"])
			}
			if v["members"] == "yes" {
				args = append(args, "--members")
			}
			return append(args, "--level", v["level"]), nil
		},
	},
	{
		id:          "clone-pipeline",
		description: "Copies a release pipeline to a new name/folder in the same project.",
		newFields: func() []*field {
			return []*field{
				textField("source", "Source pipeline path", `e.g. Example.Project\DEV\CONFIG\App\Svc`, true).
					withHelp("Existing pipeline to copy from."),
				textField("destination", "New pipeline path", `e.g. Example.Project\TEST\CONFIG\App\Svc`, true).
					withHelp("Path for the new, cloned pipeline."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["source"], v["destination"], "--level", v["level"]}
			if v["dry_run"] == "yes" {
				return append(args, "--dry-run"), nil
			}
			return append(args, "--yes"), nil
		},
	},
	{
		id:          "cancel-releases",
		description: "Cancels in-progress/queued deployments of releases under a path (batch).",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose releases should be cancelled."),
				textField("stage", "Stage filter (optional)", "e.g. Development", false).
					withHelp("Only cancel deployments to this stage. Leave empty for every stage."),
				textField("top", "Releases to inspect", "20", false).
					withHelp("How many of each pipeline's most recent releases to check for active deployments."),
				boolField("abandon", "Also abandon the releases", false).
					withHelp("Yes: abandon the releases after cancelling their deployments, not just cancel them."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			if v["top"] != "" {
				n, err := strconv.Atoi(v["top"])
				if err != nil || n < 1 {
					return nil, fmt.Errorf("releases to inspect must be a positive integer")
				}
				args = append(args, "--top", v["top"])
			}
			if v["abandon"] == "yes" {
				args = append(args, "--abandon")
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "rename-or-move-pipelines",
		description: "Bulk-renames and/or moves release pipelines to another folder.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path to rename and/or move."),
				textField("move_to", "Move to folder", `e.g. TEST\ARCHIVE (no project name)`, false).
					withHelp("Destination folder to move matching pipelines into (no project name prefix)."),
				textField("find", "Find in name", "e.g. Example.", false).
					withHelp("Substring to search for in each pipeline's name, used together with 'Replace with'."),
				textField("replace", "Replace with", "e.g. Platform.", false).
					withHelp("Replacement text for the substring found by 'Find in name'."),
				textField("name", "New name (single pipeline)", "e.g. Inspector.v2", false).
					withHelp("Exact new name for a single target pipeline (target must resolve to one pipeline)."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if v["move_to"] == "" && v["find"] == "" && v["name"] == "" {
				return nil, fmt.Errorf("specify move-to folder, find text or a new name")
			}
			args := []string{v["target"]}
			if v["move_to"] != "" {
				args = append(args, "--move-to", v["move_to"])
			}
			if v["find"] != "" {
				args = append(args, "--find", v["find"], "--replace", v["replace"])
			}
			if v["name"] != "" {
				args = append(args, "--name", v["name"])
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "update-pipeline-variables",
		description: "Bulk-sets or removes pipeline/stage variables of release pipelines under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose variables should be updated."),
				textField("set", "Set (NAME=VALUE;...)", "Env=test;Url=http://x", false).
					withHelp("Variables to add or update, as NAME=VALUE pairs separated by ';'."),
				boolField("secret", "Mark set variables secret", false).
					withHelp("Yes: store the variables listed in 'Set' as secret values."),
				textField("remove", "Remove names (;-separated)", "OldVar;Other", false).
					withHelp("Variable names to delete, separated by ';'. At least one of this or 'Set' is required."),
				choiceField("scope", "Scope", []string{"pipeline", "stage"}, 0).
					withHelp("Whether the variables live at the pipeline level or a specific stage's level."),
				textField("stage", "Stage filter (optional)", "e.g. Development", false).
					withHelp("Which stage to target when scope is 'stage'."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			sets, removes := splitList(v["set"]), splitList(v["remove"])
			if len(sets) == 0 && len(removes) == 0 {
				return nil, fmt.Errorf("specify at least one variable to set or remove")
			}
			args := []string{v["target"], "--scope", v["scope"]}
			for _, s := range sets {
				args = append(args, "--set", s)
			}
			for _, r := range removes {
				args = append(args, "--remove", r)
			}
			if v["secret"] == "yes" {
				args = append(args, "--secret")
			}
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "update-pipeline-agent-job",
		description: "Bulk-updates agent job timeouts and agent pool of release pipelines under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose agent job settings should be updated."),
				textField("timeout", "Timeout (min)", "e.g. 120 (0 = none)", false).
					withHelp("Agent job timeout in minutes. Use 0 for no timeout."),
				textField("cancel_timeout", "Cancel timeout (min)", "e.g. 5", false).
					withHelp("How long (minutes) a job is given to clean up after being cancelled."),
				textField("pool", "Agent pool name", "e.g. TestPool", false).
					withHelp("New agent pool to run on, by name. Use this or queue id below, not both."),
				textField("queue_id", "Agent queue id", "alternative to pool name", false).
					withHelp("New agent pool to run on, by queue id. Use this or pool name above, not both."),
				textField("stage", "Stage filter (optional)", "e.g. Development", false).
					withHelp("Only update this stage's agent job. Leave empty for every stage."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			for _, f := range []struct{ key, flag string }{
				{"timeout", "--timeout"}, {"cancel_timeout", "--job-cancel-timeout"},
				{"pool", "--pool"}, {"queue_id", "--queue-id"}, {"stage", "--stage"},
			} {
				if v[f.key] != "" {
					args = append(args, f.flag, v[f.key])
				}
			}
			if v["timeout"] == "" && v["cancel_timeout"] == "" && v["pool"] == "" && v["queue_id"] == "" {
				return nil, fmt.Errorf("specify at least one of timeout, cancel timeout, pool or queue id")
			}
			if v["pool"] != "" && v["queue_id"] != "" {
				return nil, fmt.Errorf("use either pool name or queue id, not both")
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "update-pipeline-schedule",
		description: "Bulk-sets or removes (pauses) scheduled triggers of release pipelines under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose scheduled trigger should be updated."),
				choiceField("action", "Action", []string{"set", "remove"}, 0).
					withHelp("'set' creates/updates the schedule; 'remove' pauses it."),
				textField("time", "Time (HH:MM, for set)", "e.g. 02:30", false).
					withHelp("Time of day to trigger the release. Required when action is 'set'."),
				textField("days", "Days (for set)", "all | weekdays | mon,wed,fri", false).
					withHelp("Which days to trigger on: 'all', 'weekdays', or a comma-separated list."),
				textField("timezone", "Time zone (for set)", "e.g. Turkey Standard Time", false).
					withHelp("Time zone the schedule's time is interpreted in."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--action", v["action"]}
			if v["action"] == "set" {
				if v["time"] == "" {
					return nil, fmt.Errorf("time is required for action 'set'")
				}
				args = append(args, "--time", v["time"])
				if v["days"] != "" {
					args = append(args, "--days", v["days"])
				}
				if v["timezone"] != "" {
					args = append(args, "--timezone", v["timezone"])
				}
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "restore-pipelines",
		description: "Recreates or overwrites release pipelines from backup-pipelines JSON files.",
		newFields: func() []*field {
			return []*field{
				textField("backup_path", "Backup file or folder", "e.g. ./backups/Example.Project/TEST/CONFIG", true).
					withHelp("A single backup JSON file, or a folder to restore every *.json backup found under it."),
				textField("filter", "Filename filter (optional)", "text in backup filename", false).
					withHelp("Only backup files whose filename contains this text."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["backup_path"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "trigger-release",
		description: "Creates new releases for all release pipelines under a path (batch).",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path to create releases for."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				textField("description", "Description (optional)", "stored on each release", false).
					withHelp("Description stored on each release that gets created."),
				textField("stages", "Manual stages (;-separated)", "e.g. Development", false).
					withHelp("Stages to deploy immediately instead of leaving for manual approval, separated by ';'."),
				textField("interval", "Interval seconds (optional)", "e.g. 2 (empty = all at once)", false).
					withHelp("Seconds to wait between consecutive releases. Leave empty to trigger every release at once."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			if v["description"] != "" {
				args = append(args, "--description", v["description"])
			}
			for _, s := range splitList(v["stages"]) {
				args = append(args, "--stage", s)
			}
			if v["interval"] != "" {
				n, err := strconv.ParseFloat(v["interval"], 64)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("interval seconds must be a non-negative number")
				}
				args = append(args, "--interval", v["interval"])
			}
			return writeTail(args, v), nil
		},
	},
}

// writeLevels returns the PAT levels accepted by commands that mutate pipeline definitions.
func writeLevels() []string { return []string{"read-write", "manage"} }

var commandSpecs = []commandSpec{
	{
		id:          "compare-pipelines",
		description: "Compares two release pipelines (variables, agent job settings, tasks).",
		newFields: func() []*field {
			return []*field{
				textField("pipeline_a", "Pipeline A path", `e.g. Example.Project\DEV\CONFIG\DEVAPP\Example.Service`, true).
					withHelp("First pipeline in the comparison."),
				textField("pipeline_b", "Pipeline B path", `e.g. Example.Project\TEST\CONFIG\TESTAPP\Example.Service`, true).
					withHelp("Second pipeline in the comparison."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["pipeline_a"], v["pipeline_b"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "compare-folders",
		description: "Compares the release pipelines under two folders: counts, names, and content.",
		newFields: func() []*field {
			return []*field{
				textField("folder_a", "Folder A path", `e.g. Example.Project\DEV\CONFIG`, true).
					withHelp("First folder in the comparison."),
				textField("folder_b", "Folder B path", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Second folder in the comparison."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				boolField("names_only", "Names only (skip content diff)", false).
					withHelp("Yes: only compare pipeline counts and names. No: also diff the content of pipelines present on both sides."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["folder_a"], v["folder_b"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			if v["names_only"] == "yes" {
				args = append(args, "--names-only")
			}
			return append(args, "--level", v["level"]), nil
		},
	},
	{
		id:          "create-files",
		description: "Creates empty file(s) in a target folder, creating the folder if needed.",
		newFields: func() []*field {
			return []*field{
				textField("target_path", "Target folder", "e.g. targetpath", true).
					withHelp("Folder to create the files in. Created automatically if it doesn't exist."),
				textField("filenames", "Filenames (space-separated)", "file1.txt file2.py sub/folder/file3.txt", true).
					withHelp("One or more filenames to create, separated by spaces. Subfolders in a name are created as needed."),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			filenames := strings.Fields(v["filenames"])
			if len(filenames) == 0 {
				return nil, fmt.Errorf("at least one filename is required")
			}
			return append([]string{v["target_path"]}, filenames...), nil
		},
	},
	{
		id:          "list-releases",
		description: "Lists Azure DevOps release pipelines and their folders.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (project/folder)", `e.g. Example.Project or 'Example.Project\TEST\CONFIG'`, true).
					withHelp("Project name, or a 'Project\\Folder' path, to list pipelines under."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["target"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "list-pipeline-agent-job",
		description: "Lists the agent job settings (pool, demands, timeout, etc.) of a release pipeline.",
		newFields: func() []*field {
			return []*field{
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true).
					withHelp("Single pipeline to inspect."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["pipeline_path"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "list-pipeline-schedule",
		description: "Lists the scheduled triggers for a release pipeline.",
		newFields: func() []*field {
			return []*field{
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true).
					withHelp("Single pipeline to inspect."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["pipeline_path"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "list-pipeline-steps",
		description: "Lists the stages/tasks and script contents of a release pipeline.",
		newFields: func() []*field {
			return []*field{
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true).
					withHelp("Single pipeline to inspect."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["pipeline_path"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "list-pipeline-variables",
		description: "Lists the pipeline-level and stage-level variables of a release pipeline.",
		newFields: func() []*field {
			return []*field{
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true).
					withHelp("Single pipeline to inspect."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["pipeline_path"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "list-release-history",
		description: "Lists the most recent releases of a pipeline with creator, date and stage statuses.",
		newFields: func() []*field {
			return []*field{
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true).
					withHelp("Single pipeline to inspect."),
				textField("top", "Number of releases", "10", false).
					withHelp("How many of the most recent releases to list."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["pipeline_path"], "--level", v["level"]}
			if v["top"] != "" {
				n, err := strconv.Atoi(v["top"])
				if err != nil || n < 1 {
					return nil, fmt.Errorf("number of releases must be a positive integer")
				}
				args = append(args, "--top", v["top"])
			}
			return args, nil
		},
	},
	{
		id:          "list-release-status",
		description: "Lists the succeeded/failed status of the latest run of each release pipeline under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true).
					withHelp("Folder or single pipeline path to check."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["target"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "update-pipeline-demands",
		description: "Bulk-updates the demands of release pipelines under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose demands should be updated."),
				textField("demand", "Demand rule(s) (';'-separated)", "Agent.Name -equals build-agent-01", false).
					withHelp("Demand rule(s) to apply, separated by ';'. Required unless 'Clear demands entirely' is enabled."),
				boolField("clear", "Clear demands entirely", false).
					withHelp("Yes: remove all demands instead of setting/adding/removing specific rules."),
				choiceField("mode", "Mode", []string{"set", "add", "remove"}, 0).
					withHelp("'set' replaces existing demands, 'add' appends, 'remove' deletes the listed ones."),
				textField("stage", "Stage filter (optional)", "e.g. Development", false).
					withHelp("Only update this stage's agent job demands. Leave empty for every stage."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			clear := v["clear"] == "yes"

			var demands []string
			for _, d := range strings.Split(v["demand"], ";") {
				d = strings.TrimSpace(d)
				if d != "" {
					demands = append(demands, d)
				}
			}
			if len(demands) == 0 && !clear {
				return nil, fmt.Errorf("specify at least one demand or enable 'Clear demands entirely'")
			}

			args := []string{v["target"]}
			for _, d := range demands {
				args = append(args, "--demand", d)
			}
			if clear {
				args = append(args, "--clear")
			}
			args = append(args, "--mode", v["mode"])
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			args = append(args, "--level", v["level"])
			if v["dry_run"] == "yes" {
				args = append(args, "--dry-run")
			} else {
				// The TUI's own Run action is the confirmation; skip the subprocess' interactive
				// stdin prompt since it isn't attached to a terminal here.
				args = append(args, "--yes")
			}
			return args, nil
		},
	},
	{
		id:          "backup-pipelines",
		description: "Saves matching release pipelines' definitions to local JSON files.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path to back up."),
				textField("out", "Output folder", "e.g. ./backups", true).
					withHelp("Local folder to write one JSON file per pipeline into (project/folder structure is mirrored)."),
				textField("filter", "Name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if v["out"] == "" {
				return nil, fmt.Errorf("output folder is required")
			}
			args := []string{v["target"], "--out", v["out"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			args = append(args, "--level", v["level"])
			if v["dry_run"] == "yes" {
				args = append(args, "--dry-run")
			}
			return args, nil
		},
	},
}
