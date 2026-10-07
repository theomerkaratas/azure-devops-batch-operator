// Command synchronize-pipelines copies selected components from a reference release pipeline.
package synchronizepipelines

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type components map[string]bool

const usageEpilog = `
Examples:
  synchronize-pipelines 'Example.Project\TEST' --reference 'Example.Project\GOLDEN\Deploy' --components steps,agent-settings --dry-run
  synchronize-pipelines 'Example.Project\TEST' --reference 'Example.Project\GOLDEN\Deploy' --components variables,approvals -y
Components:
  variables       Pipeline variables and variables on name-matched stages. Masked secret values are preserved, never copied.
  steps           Tasks in name-matched stages and jobs.
  jobs            Jobs in name-matched stages (includes their steps and agent settings).
  stages          The complete stage collection (preserves destination IDs for stages/jobs with matching names).
  agent-settings  deploymentInput on name-matched stages and jobs.
  approvals       Pre- and post-deployment approval settings on name-matched stages.
  all             All components above.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("synchronize-pipelines", flag.ExitOnError)
	reference := fs.String("reference", "", "Full path of the reference pipeline")
	componentList := fs.String("components", "", "Comma-separated components to synchronize")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists planned changes without saving")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Synchronizes selected components from a reference release pipeline.")
		fmt.Fprintln(os.Stderr, "\nUsage: synchronize-pipelines <target> --reference <pipeline> --components <list> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"reference": true, "components": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 || *reference == "" || *componentList == "" {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	selected, err := parseComponents(*componentList)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *reference, *filter, *level, *dryRun, *yes, selected); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, reference, filter, level string, dryRun, autoYes bool, selected components) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	refProject, refPath, refName, err := azuredevops.ParsePipelinePath(reference)
	if err != nil {
		return fmt.Errorf("reference: %w", err)
	}
	targetProject := strings.Split(strings.ReplaceAll(target, "/", `\`), `\`)[0]
	if !strings.EqualFold(targetProject, refProject) && (selected["agent-settings"] || selected["jobs"] || selected["stages"]) {
		return fmt.Errorf("agent settings cannot be synchronized across projects because queue IDs are project-specific; use a reference pipeline in %s", targetProject)
	}
	refDefinition, err := cfg.FindDefinition(refProject, refPath, refName)
	if err != nil {
		return err
	}
	refRaw, err := cfg.GetDefinitionDetailRaw(refProject, refDefinition.ID)
	if err != nil {
		return fmt.Errorf("read reference pipeline: %w", err)
	}
	mutate := func(_ azuredevops.Config, project string, raw map[string]interface{}) []string {
		if project == refProject && numericID(raw["id"]) == refDefinition.ID {
			return nil
		}
		return synchronize(raw, refRaw, selected)
	}
	opts := batchupdate.Options{Target: target, Filter: filter, Level: level, DryRun: dryRun, AutoYes: autoYes}
	return batchupdate.Run(opts, "Synchronized selected components from "+reference, mutate)
}

func parseComponents(value string) (components, error) {
	valid := map[string]bool{"variables": true, "steps": true, "jobs": true, "stages": true, "agent-settings": true, "approvals": true}
	out := components{}
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "all" {
			for name := range valid {
				out[name] = true
			}
			continue
		}
		if !valid[item] {
			return nil, fmt.Errorf("unknown component %q", item)
		}
		out[item] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--components must not be empty")
	}
	return out, nil
}

func synchronize(target, reference map[string]interface{}, selected components) []string {
	var changes []string
	if selected["variables"] && syncVariables(target, reference) {
		changes = append(changes, "synchronize pipeline variables")
	}
	if selected["stages"] {
		stages := reboundStages(reference["environments"], target["environments"])
		if !reflect.DeepEqual(target["environments"], stages) {
			target["environments"] = stages
			changes = append(changes, "synchronize complete stage collection")
		}
	} else {
		targetStages := namedMaps(target["environments"])
		for name, refStage := range namedMaps(reference["environments"]) {
			stage := targetStages[name]
			if stage == nil {
				continue
			}
			if selected["variables"] && syncVariables(stage, refStage) {
				changes = append(changes, fmt.Sprintf("[%s] synchronize variables", name))
			}
			if selected["approvals"] && syncApprovals(stage, refStage) {
				changes = append(changes, fmt.Sprintf("[%s] synchronize approvals", name))
			}
			if selected["jobs"] {
				jobs := reboundJobs(refStage["deployPhases"], stage["deployPhases"])
				if !reflect.DeepEqual(stage["deployPhases"], jobs) {
					stage["deployPhases"] = jobs
					changes = append(changes, fmt.Sprintf("[%s] synchronize jobs", name))
				}
				continue
			}
			targetJobs := namedMaps(stage["deployPhases"])
			for jobName, refJob := range namedMaps(refStage["deployPhases"]) {
				job := targetJobs[jobName]
				if job == nil {
					continue
				}
				if selected["steps"] && copyKeys(job, refJob, "workflowTasks") {
					changes = append(changes, fmt.Sprintf("[%s / %s] synchronize steps", name, jobName))
				}
				if selected["agent-settings"] && copyKeys(job, refJob, "deploymentInput") {
					changes = append(changes, fmt.Sprintf("[%s / %s] synchronize agent settings", name, jobName))
				}
			}
		}
	}
	sort.Strings(changes)
	return changes
}

func syncVariables(target, reference map[string]interface{}) bool {
	refVars, _ := reference["variables"].(map[string]interface{})
	targetVars, _ := target["variables"].(map[string]interface{})
	next := map[string]interface{}{}
	for name, value := range refVars {
		variable, _ := value.(map[string]interface{})
		secret, _ := variable["isSecret"].(bool)
		if secret {
			if existing, ok := targetVars[name]; ok {
				next[name] = clone(existing)
			}
			continue
		}
		next[name] = clone(value)
	}
	if len(targetVars) == 0 && len(next) == 0 || reflect.DeepEqual(targetVars, next) {
		return false
	}
	target["variables"] = next
	return true
}

func copyKeys(target, source map[string]interface{}, keys ...string) bool {
	changed := false
	for _, key := range keys {
		value, exists := source[key]
		if !exists {
			value = nil
		}
		value = clone(value)
		if !reflect.DeepEqual(target[key], value) {
			target[key] = value
			changed = true
		}
	}
	return changed
}

func namedMaps(value interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	items, _ := value.([]interface{})
	for _, item := range items {
		m, _ := item.(map[string]interface{})
		name, _ := m["name"].(string)
		if m != nil && name != "" {
			out[name] = m
		}
	}
	return out
}

func reboundStages(source, destination interface{}) interface{} {
	result, _ := clone(source).([]interface{})
	destStages := namedMaps(destination)
	for _, item := range result {
		stage, _ := item.(map[string]interface{})
		name, _ := stage["name"].(string)
		dest := destStages[name]
		if dest == nil {
			delete(stage, "id")
			clearApprovalIDs(stage)
			continue
		}
		if id, ok := dest["id"]; ok {
			stage["id"] = id
		} else {
			delete(stage, "id")
		}
		stage["deployPhases"] = reboundJobs(stage["deployPhases"], dest["deployPhases"])
		rebindApprovalIDs(stage, dest)
	}
	return result
}

func reboundJobs(source, destination interface{}) interface{} {
	result, _ := clone(source).([]interface{})
	destJobs := namedMaps(destination)
	for name, job := range namedMaps(result) {
		if dest := destJobs[name]; dest != nil {
			if id, ok := dest["id"]; ok {
				job["id"] = id
			} else {
				delete(job, "id")
			}
		} else {
			delete(job, "id")
		}
	}
	return result
}

func syncApprovals(target, source map[string]interface{}) bool {
	changed := false
	for _, key := range []string{"preDeployApprovals", "postDeployApprovals"} {
		value := clone(source[key])
		wrapper, _ := value.(map[string]interface{})
		destination, _ := target[key].(map[string]interface{})
		rebindApprovalList(wrapper, destination)
		if !reflect.DeepEqual(target[key], value) {
			target[key] = value
			changed = true
		}
	}
	return changed
}

func rebindApprovalIDs(target, destination map[string]interface{}) {
	for _, key := range []string{"preDeployApprovals", "postDeployApprovals"} {
		targetBlock, _ := target[key].(map[string]interface{})
		destBlock, _ := destination[key].(map[string]interface{})
		rebindApprovalList(targetBlock, destBlock)
	}
}

func clearApprovalIDs(stage map[string]interface{}) {
	rebindApprovalIDs(stage, map[string]interface{}{})
}

func rebindApprovalList(target, destination map[string]interface{}) {
	if target == nil {
		return
	}
	targetItems, _ := target["approvals"].([]interface{})
	destItems, _ := destination["approvals"].([]interface{})
	for i, item := range targetItems {
		approval, _ := item.(map[string]interface{})
		if approval == nil {
			continue
		}
		if i < len(destItems) {
			dest, _ := destItems[i].(map[string]interface{})
			if id, ok := dest["id"]; ok {
				approval["id"] = id
				continue
			}
		}
		delete(approval, "id")
	}
}

func clone(value interface{}) interface{} {
	data, _ := json.Marshal(value)
	var out interface{}
	_ = json.Unmarshal(data, &out)
	return out
}

func numericID(value interface{}) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return 0
}
