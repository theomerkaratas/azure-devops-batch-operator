package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

// fieldKind distinguishes free-text input fields from fixed-choice fields.
type fieldKind int

const (
	fieldText fieldKind = iota
	fieldChoice
)

// field is one input of a command's form. Text fields are backed by a textinput.Model;
// choice fields cycle through a fixed set of values with the left/right keys.
type field struct {
	key       string
	label     string
	kind      fieldKind
	required  bool
	choices   []string
	choiceIdx int
	input     textinput.Model
	pick      pickMode // non-zero: enter opens the path picker
}

// pickModes marks which form field keys are Azure DevOps paths and what the picker accepts.
var pickModes = map[string]pickMode{
	"target":        pickAny,
	"pipeline_path": pickPipeline,
	"pipeline_a":    pickPipeline,
	"pipeline_b":    pickPipeline,
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
		id:          "delete-pipelines",
		description: "Permanently deletes matching classic release pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\ARCHIVE`, true),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				textField("comment", "Audit comment", "Deleted by azure-devops-batch-operator", false),
				boolField("force", "Force (cancel active deployments)", false),
				choiceField("level", "PAT level", []string{"manage"}, 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("pool", "Pool name (optional)", "empty = every pool; partial names are accepted", false),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("pool", "Pool name (optional)", "empty = all pools; text = list its members", false),
				boolField("members", "List members of all pools", false),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("source", "Source pipeline path", `e.g. Example.Project\DEV\CONFIG\App\Svc`, true),
				textField("destination", "New pipeline path", `e.g. Example.Project\TEST\CONFIG\App\Svc`, true),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				textField("stage", "Stage filter (optional)", "e.g. Development", false),
				textField("top", "Releases to inspect", "20", false),
				boolField("abandon", "Also abandon the releases", false),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				textField("move_to", "Move to folder", `e.g. TEST\ARCHIVE (no project name)`, false),
				textField("find", "Find in name", "e.g. Example.", false),
				textField("replace", "Replace with", "e.g. Platform.", false),
				textField("name", "New name (single pipeline)", "e.g. Inspector.v2", false),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				textField("set", "Set (NAME=VALUE;...)", "Env=test;Url=http://x", false),
				boolField("secret", "Mark set variables secret", false),
				textField("remove", "Remove names (;-separated)", "OldVar;Other", false),
				choiceField("scope", "Scope", []string{"pipeline", "stage"}, 0),
				textField("stage", "Stage filter (optional)", "e.g. Development", false),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				textField("timeout", "Timeout (min)", "e.g. 120 (0 = none)", false),
				textField("cancel_timeout", "Cancel timeout (min)", "e.g. 5", false),
				textField("pool", "Agent pool name", "e.g. TestPool", false),
				textField("queue_id", "Agent queue id", "alternative to pool name", false),
				textField("stage", "Stage filter (optional)", "e.g. Development", false),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				choiceField("action", "Action", []string{"set", "remove"}, 0),
				textField("time", "Time (HH:MM, for set)", "e.g. 02:30", false),
				textField("days", "Days (for set)", "all | weekdays | mon,wed,fri", false),
				textField("timezone", "Time zone (for set)", "e.g. Turkey Standard Time", false),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
		id:          "trigger-release",
		description: "Creates new releases for all release pipelines under a path (batch).",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				textField("filter", "Name filter (optional)", "text in pipeline name", false),
				textField("description", "Description (optional)", "stored on each release", false),
				textField("stages", "Manual stages (;-separated)", "e.g. Development", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
				textField("pipeline_a", "Pipeline A path", `e.g. Example.Project\DEV\CONFIG\DEVAPP\Example.Service`, true),
				textField("pipeline_b", "Pipeline B path", `e.g. Example.Project\TEST\CONFIG\TESTAPP\Example.Service`, true),
				choiceField("level", "PAT level", readLevels(), 0),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return []string{v["pipeline_a"], v["pipeline_b"], "--level", v["level"]}, nil
		},
	},
	{
		id:          "create-files",
		description: "Creates empty file(s) in a target folder, creating the folder if needed.",
		newFields: func() []*field {
			return []*field{
				textField("target_path", "Target folder", "e.g. targetpath", true),
				textField("filenames", "Filenames (space-separated)", "file1.txt file2.py sub/folder/file3.txt", true),
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
				textField("target", "Target (project/folder)", `e.g. Example.Project or 'Example.Project\TEST\CONFIG'`, true),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("pipeline_path", "Pipeline path", `e.g. Example.Project\PREP\CONFIG\PREPAPP\Example.API`, true),
				textField("top", "Number of releases", "10", false),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				choiceField("level", "PAT level", readLevels(), 0),
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
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true),
				textField("demand", "Demand rule(s) (';'-separated)", "Agent.Name -equals build-agent-01", false),
				boolField("clear", "Clear demands entirely", false),
				choiceField("mode", "Mode", []string{"set", "add", "remove"}, 0),
				textField("stage", "Stage filter (optional)", "e.g. Development", false),
				choiceField("level", "PAT level", writeLevels(), 0),
				boolField("dry_run", "Dry run (preview only)", true),
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
}
