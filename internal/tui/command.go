package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
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
	"target":             pickAny,
	"pipeline_path":      pickPipeline,
	"pipeline_a":         pickPipeline,
	"pipeline_b":         pickPipeline,
	"folder_a":           pickAny,
	"folder_b":           pickAny,
	"source":             pickPipeline,
	"reference":          pickPipeline,
	"source_pipeline":    pickPipeline,
	"destination":        pickNewPipeline,
	"source_folder":      pickProjectFolder,
	"destination_folder": pickNewFolder,
	"move_to":            pickFolder,
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
		id: "orchestrate-releases", description: "Creates releases in controlled waves with concurrency, waiting, retries, and a summary.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				textField("wave_size", "Pipelines per wave", "5", false), textField("concurrency", "Max simultaneous per wave (optional)", "e.g. 2", false),
				textField("wave_delay", "Seconds between waves", "0", false),
				boolField("wait", "Wait for each deployment to finish", false), boolField("stop", "Stop after the first failure", true),
				textField("timeout", "Wait timeout (minutes)", "60", false), textField("retries", "Retries for transient errors", "2", false),
				textField("stage", "Manual stages (;-separated, optional)", "e.g. Production", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			for _, opt := range [][3]string{{"--wave-size", "wave_size", "1"}, {"--concurrency", "concurrency", "0"}, {"--retries", "retries", "0"}} {
				if v[opt[1]] == "" {
					continue
				}
				min, _ := strconv.Atoi(opt[2])
				if n, err := strconv.Atoi(v[opt[1]]); err != nil || n < min {
					return nil, fmt.Errorf("invalid %s %q: expected a whole number of at least %d", opt[1], v[opt[1]], min)
				}
				args = append(args, opt[0], v[opt[1]])
			}
			for _, opt := range [][2]string{{"--wave-delay", "wave_delay"}, {"--timeout", "timeout"}} {
				if v[opt[1]] != "" {
					if n, err := strconv.ParseFloat(v[opt[1]], 64); err != nil || n < 0 {
						return nil, fmt.Errorf("invalid %s %q", opt[1], v[opt[1]])
					}
					args = append(args, opt[0], v[opt[1]])
				}
			}
			if v["wait"] == "yes" {
				args = append(args, "--wait")
			}
			if v["stop"] == "yes" {
				args = append(args, "--stop-on-failure")
			}
			for _, s := range splitList(v["stage"]) {
				args = append(args, "--stage", s)
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "update-pipeline-retention", description: "Standardizes release retention (days, release count, build retention) per stage.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				textField("days", "Days to keep (optional)", "e.g. 30", false), textField("releases", "Releases to keep (optional)", "e.g. 5", false),
				choiceField("retain_build", "Retain associated builds", []string{"unchanged", "true", "false"}, 0),
				textField("stage", "Stages (comma-separated, optional)", "e.g. Production", false).withHelp("Leave empty to change every stage."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			for _, opt := range [][2]string{{"--days", "days"}, {"--releases", "releases"}} {
				if v[opt[1]] != "" {
					if n, err := strconv.Atoi(v[opt[1]]); err != nil || n <= 0 {
						return nil, fmt.Errorf("invalid %s %q", opt[1], v[opt[1]])
					}
					args = append(args, opt[0], v[opt[1]])
				}
			}
			if v["retain_build"] != "unchanged" {
				args = append(args, "--retain-build", v["retain_build"])
			}
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			if v["days"] == "" && v["releases"] == "" && v["retain_build"] == "unchanged" {
				return nil, fmt.Errorf("set days, releases, or build retention")
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "cleanup-releases", description: "Deletes old release instances by age, status, and retention counts (not restorable with this tool).",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				textField("older_than", "Older than (days, optional)", "e.g. 90", false),
				textField("status", "Statuses (comma-separated, optional)", "succeeded,failed,canceled,abandoned,draft,notdeployed", false).withHelp("At least one of 'older than' or statuses is required."),
				textField("keep_latest", "Always keep newest N per pipeline", "3", false),
				textField("keep_successful", "Always keep newest N succeeded per pipeline", "0", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"manage"}, 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp("Yes: list what would be deleted. No: delete the listed releases (they cannot be restored with this tool; Azure DevOps destroys them for good after the project's retention period). Releases kept forever or in progress are never deleted."),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if v["older_than"] == "" && v["status"] == "" {
				return nil, fmt.Errorf("set 'older than' and/or statuses")
			}
			args := []string{v["target"], "--level", v["level"]}
			for _, opt := range [][2]string{{"--older-than", "older_than"}, {"--status", "status"}, {"--keep-latest", "keep_latest"}, {"--keep-successful", "keep_successful"}, {"--filter", "filter"}} {
				if v[opt[1]] != "" {
					args = append(args, opt[0], v[opt[1]])
				}
			}
			if v["dry_run"] == "yes" {
				return append(args, "--dry-run"), nil
			}
			return append(args, "--apply", "--yes"), nil
		},
	},
	{
		id: "upgrade-pipeline-tasks", description: "Upgrades a task to another major version after checking input compatibility.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				textField("task", "Task (name or ID)", "e.g. PowerShell", true).withHelp("Task name, friendly name, or GUID as known to the organization."),
				textField("to_version", "Upgrade to major version", "e.g. 2", true),
				textField("from_version", "Only from major version (optional)", "e.g. 1", false),
				boolField("force", "Force (ignore input problems and downgrades)", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if n, err := strconv.Atoi(v["to_version"]); err != nil || n <= 0 {
				return nil, fmt.Errorf("invalid target version %q", v["to_version"])
			}
			args := []string{v["target"], "--task", v["task"], "--to-version", v["to_version"]}
			if v["from_version"] != "" {
				if n, err := strconv.Atoi(v["from_version"]); err != nil || n <= 0 {
					return nil, fmt.Errorf("invalid source version %q", v["from_version"])
				}
				args = append(args, "--from-version", v["from_version"])
			}
			if v["force"] == "yes" {
				args = append(args, "--force")
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "update-pipeline-cd-triggers", description: "Enables, disables, or filters continuous-deployment triggers on build artifacts.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				choiceField("action", "Action", []string{"enable", "disable", "set-filters"}, 0).withHelp("enable: create the trigger. disable: remove it. set-filters: replace filters of existing triggers."),
				textField("alias", "Artifact aliases (comma-separated, optional)", "e.g. _Build", false).withHelp("Leave empty for every Build artifact."),
				textField("branch", "Build branch filters (comma-separated)", "e.g. refs/heads/main", false),
				textField("tag", "Build tag filters (comma-separated)", "e.g. release", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--action", v["action"]}
			for _, opt := range [][2]string{{"--alias", "alias"}, {"--branch", "branch"}, {"--tag", "tag"}} {
				if v[opt[1]] != "" {
					args = append(args, opt[0], v[opt[1]])
				}
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "update-pipeline-stage-triggers", description: "Changes when stages start: after release, after stages, or manually.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				choiceField("trigger", "Trigger", []string{"sequential", "after-release", "after-stages", "manual"}, 0).withHelp("sequential: each stage follows the previous one. after-stages: wait for the stages in 'After stages'."),
				textField("stage", "Stages to change (comma-separated, optional)", "e.g. Prod,QA", false).withHelp("Leave empty to change every stage."),
				textField("after", "After stages (comma-separated)", "e.g. QA,Perf", false).withHelp("Only for the after-stages trigger."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--trigger", v["trigger"]}
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			if v["after"] != "" {
				args = append(args, "--after", v["after"])
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "update-pipeline-artifacts", description: "Replaces build artifact sources, branches, projects, or aliases.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				textField("match_alias", "Match alias (optional)", "e.g. _OldBuild", false), textField("match_definition", "Match build pipeline (optional)", "e.g. OldBuild", false),
				textField("match_project", "Match project (optional)", "e.g. Example.Project", false), textField("match_branch", "Match branch (optional)", "e.g. refs/heads/main", false),
				textField("set_definition", "New build pipeline (optional)", "e.g. NewBuild", false), textField("set_project", "New project (optional)", "e.g. Other.Project", false),
				textField("set_branch", "New default branch (optional)", "e.g. refs/heads/release", false), textField("set_alias", "New alias (optional)", "e.g. _NewBuild", false),
				boolField("rewrite", "Rewrite alias references in stages/steps", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			for _, opt := range [][2]string{{"--match-alias", "match_alias"}, {"--match-definition", "match_definition"}, {"--match-project", "match_project"}, {"--match-branch", "match_branch"}, {"--set-definition", "set_definition"}, {"--set-project", "set_project"}, {"--set-branch", "set_branch"}, {"--set-alias", "set_alias"}} {
				if v[opt[1]] != "" {
					args = append(args, opt[0], v[opt[1]])
				}
			}
			if v["rewrite"] == "yes" {
				args = append(args, "--rewrite-references")
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "manage-pipeline-stages", description: "Adds, clones, renames, removes, or reorders stages across release pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				choiceField("action", "Action", []string{"add", "clone", "rename", "remove", "reorder"}, 0).withHelp("add: empty stage. clone: duplicate --stage. rename: rename and update dependents. remove: delete (rewire fixes dependents). reorder: move."),
				textField("stage", "Stage name", "e.g. QA", true).withHelp("The stage to add, clone, rename, remove, or move."),
				textField("new_name", "New name (clone/rename)", "e.g. Staging", false),
				textField("after", "Place after stage (optional)", "e.g. Dev", false).withHelp("Used by add, clone and reorder. Leave empty for the default placement."),
				textField("position", "Position (optional, 1-based)", "e.g. 1", false).withHelp("Alternative to 'place after'."),
				textField("depends_on", "Depends on (comma-separated, optional)", "e.g. Dev,QA", false),
				boolField("rewire", "Rewire dependents on remove", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--action", v["action"], "--stage", v["stage"]}
			for _, opt := range [][2]string{{"--new-name", "new_name"}, {"--after", "after"}, {"--position", "position"}, {"--depends-on", "depends_on"}} {
				if v[opt[1]] != "" {
					args = append(args, opt[0], v[opt[1]])
				}
			}
			if v["rewire"] == "yes" {
				args = append(args, "--rewire")
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "copy-pipeline-stage", description: "Copies one complete stage from a source pipeline into target pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true),
				textField("source_pipeline", "Source pipeline", `e.g. Example.Project\GOLDEN\Deploy`, true).withHelp("Pipeline that holds the stage to copy."),
				textField("stage", "Stage name", "e.g. Production", true),
				choiceField("on_existing", "If the stage already exists", []string{"skip", "replace", "rename"}, 0),
				textField("new_name", "New name (rename/optional)", "e.g. Production-copy", false),
				textField("after", "Place after stage (optional)", "e.g. Dev", false),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--source", v["source_pipeline"], "--stage", v["stage"], "--on-existing", v["on_existing"]}
			if v["new_name"] != "" {
				args = append(args, "--new-name", v["new_name"])
			}
			if v["after"] != "" {
				args = append(args, "--after", v["after"])
			}
			return writeTail(args, v), nil
		},
	},
	{
		id: "synchronize-pipelines", description: "Copies selected components from a reference release pipeline into matching pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true).withHelp("Pipelines that receive the reference components."),
				textField("reference", "Reference pipeline", `e.g. Example.Project\GOLDEN\Deploy`, true).withHelp("Golden pipeline used as the source of truth."),
				textField("components", "Components (comma-separated)", "steps,agent-settings", true).withHelp("variables, steps, jobs, stages, agent-settings, approvals, or all."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			return writeTail([]string{v["target"], "--reference", v["reference"], "--components", v["components"]}, v), nil
		},
	},
	{
		id: "update-pipeline-variable-groups", description: "Links or unlinks shared variable groups across release pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true), textField("links", "Group IDs to link (;-separated)", "e.g. 12;18", false), textField("unlinks", "Group IDs to unlink (;-separated)", "e.g. 7", false),
				choiceField("scope", "Scope", []string{"pipeline", "stage", "all"}, 0), textField("stage", "Stage name (optional)", "e.g. Production", false), textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp), boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if len(splitList(v["links"])) == 0 && len(splitList(v["unlinks"])) == 0 {
				return nil, fmt.Errorf("specify at least one group ID to link or unlink")
			}
			args := []string{v["target"], "--scope", v["scope"]}
			for _, id := range splitList(v["links"]) {
				if n, err := strconv.Atoi(id); err != nil || n <= 0 {
					return nil, fmt.Errorf("invalid variable-group ID %q", id)
				}
				args = append(args, "--link", id)
			}
			for _, id := range splitList(v["unlinks"]) {
				if n, err := strconv.Atoi(id); err != nil || n <= 0 {
					return nil, fmt.Errorf("invalid variable-group ID %q", id)
				}
				args = append(args, "--unlink", id)
			}
			if v["stage"] != "" {
				args = append(args, "--stage", v["stage"])
			}
			return writeTail(args, v), nil
		},
	},
	{
		id:          "enforce-pipeline-policy",
		description: "Enforces a YAML policy across release pipelines under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path to enforce the policy against."),
				textField("policy", "Policy YAML file", "e.g. release-policy.yml", true).
					withHelp("Local YAML file containing the desired pipeline rules."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--policy", v["policy"]}
			return writeTail(args, v), nil
		},
	},
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
		id:          "delete-pipeline-steps",
		description: "Deletes every step whose title matches exactly across release pipelines under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose steps will be searched."),
				textField("title", "Exact step title", "title is case-sensitive", true).
					withHelp("Only steps whose complete title matches this value exactly will be deleted."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--title", v["title"]}
			return writeTail(args, v), nil
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
		id:          "clone-folder",
		description: "Copies a release folder tree and every release pipeline beneath it.",
		newFields: func() []*field {
			return []*field{
				textField("source_folder", "Source folder path", `e.g. Example.Project\DEV\CONFIG`, true).
					withHelp("Existing folder to copy, including all subfolders and release pipelines."),
				textField("destination_folder", "New folder path", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("New destination folder in the same project; it must not already exist."),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["source_folder"], v["destination_folder"], "--level", v["level"]}
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
		id:          "replace-pipeline-content",
		description: "Regex-replaces matching content in scripts, step titles, and variables under a path.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path whose definitions will be searched."),
				textField("find", "Find (regular expression)", `e.g. Service-(\w+)`, true).
					withHelp("Go regular expression to search for in the selected fields."),
				textField("replace", "Replace with", `e.g. App-$1 (empty deletes)`, false).
					withHelp("Replacement text; capture groups such as $1 and ${name} are supported."),
				textField("fields", "Fields (comma-separated)", "default: scripts,titles,variables", false).
					withHelp("Select one or more of: scripts, titles, variables, or all. Empty selects all three."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				choiceField("level", "PAT level", []string{"read-write", "manage"}, 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if _, err := regexp.Compile(v["find"]); err != nil {
				return nil, fmt.Errorf("invalid find regular expression: %w", err)
			}
			fields := v["fields"]
			if fields == "" {
				fields = "scripts,titles,variables"
			}
			args := []string{v["target"], "--find", v["find"], "--replace", v["replace"], "--fields", fields}
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
		id:          "resume-batch",
		description: "Finishes an interrupted or partly failed batch operation from its saved manifest.",
		newFields: func() []*field {
			return []*field{
				textField("manifest", "Manifest ID", "e.g. 20260101-120000-ab12cd", true).
					withHelp("Printed by the original operation; list saved manifests with list-batch-manifests. Already updated pipelines are not touched again."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) { return writeTail([]string{v["manifest"]}, v), nil },
	},
	{
		id:          "rollback-batch",
		description: "Restores the definitions captured before an earlier batch operation.",
		newFields: func() []*field {
			return []*field{
				textField("manifest", "Manifest ID", "e.g. 20260101-120000-ab12cd", true).
					withHelp("Printed by the original operation. Pipelines edited since the operation are skipped, never overwritten."),
				choiceField("level", "PAT level", writeLevels(), 0).withHelp(levelHelp),
				boolField("dry_run", "Dry run (preview only)", true).withHelp(dryRunHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) { return writeTail([]string{v["manifest"]}, v), nil },
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
		id:          "audit-pipeline-permissions",
		description: "Reports who can view, edit, administer, trigger, approve, or delete pipelines and flags broad or inconsistent permissions.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true).
					withHelp("Folder or single pipeline path to audit."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				textField("broad", "Broad group name fragments (comma-separated)", "Valid Users,Everyone", false).
					withHelp("Groups whose name contains one of these are flagged when they can edit, administer, delete, or manage approvers."),
				boolField("fail", "Fail when findings exist", false).
					withHelp("Yes: return a failed command status for broad or inconsistent permissions, useful for CI."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			if v["broad"] != "" {
				args = append(args, "--broad-groups", v["broad"])
			}
			if v["fail"] == "yes" {
				args = append(args, "--fail-on-findings")
			}
			return append(args, "--level", v["level"]), nil
		},
	},
	{
		id:          "detect-deprecated-tasks",
		description: "Finds deprecated, disabled, missing, or unsupported task versions in release pipelines.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true).
					withHelp("Folder or single pipeline path to check."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				boolField("outdated", "Also report newer major versions", false),
				boolField("fail", "Fail when problems exist", false).
					withHelp("Yes: return a failed command status when at-risk tasks are found, useful for CI."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			if v["outdated"] == "yes" {
				args = append(args, "--include-outdated")
			}
			if v["fail"] == "yes" {
				args = append(args, "--fail-on-findings")
			}
			return append(args, "--level", v["level"]), nil
		},
	},
	{
		id:          "detect-broken-artifact-references",
		description: "Finds release pipelines whose build pipelines, repositories, branches, or service connections no longer exist.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST`, true).
					withHelp("Folder or single pipeline path to check."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				boolField("fail", "Fail when broken references exist", false).
					withHelp("Yes: return a failed command status when something is broken, useful for CI."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			if v["fail"] == "yes" {
				args = append(args, "--fail-on-broken")
			}
			return append(args, "--level", v["level"]), nil
		},
	},
	{
		id:          "audit-pipeline-policy",
		description: "Audits release pipelines under a path against a YAML policy.",
		newFields: func() []*field {
			return []*field{
				textField("target", "Target (folder/pipeline path)", `e.g. Example.Project\TEST\CONFIG`, true).
					withHelp("Folder or single pipeline path to audit."),
				textField("policy", "Policy YAML file", "e.g. release-policy.yml", true).
					withHelp("Local YAML file containing the required pipeline rules."),
				textField("filter", "Pipeline name filter (optional)", "text in pipeline name", false).withHelp(filterHelp),
				boolField("fail", "Fail when violations exist", false).
					withHelp("Yes: return a failed command status when violations are found, useful for CI."),
				choiceField("level", "PAT level", readLevels(), 0).withHelp(levelHelp),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			args := []string{v["target"], "--policy", v["policy"]}
			if v["filter"] != "" {
				args = append(args, "--filter", v["filter"])
			}
			if v["fail"] == "yes" {
				args = append(args, "--fail-on-violation")
			}
			return append(args, "--level", v["level"]), nil
		},
	},
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
		id:          "list-batch-manifests",
		description: "Lists saved batch operation manifests, or shows one in detail.",
		newFields: func() []*field {
			return []*field{
				textField("manifest", "Manifest ID (optional)", "empty = list all", false).
					withHelp("Show one manifest's pipelines, changes, results and failures."),
			}
		},
		buildArgs: func(v map[string]string) ([]string, error) {
			if v["manifest"] == "" {
				return nil, nil
			}
			return []string{v["manifest"]}, nil
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
