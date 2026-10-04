package azuredevops

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// IntOrString unmarshals a JSON number or a numeric string into an int. Azure DevOps returns
// daysToRelease as a string even though it is numeric.
type IntOrString int

func (v *IntOrString) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*v = IntOrString(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*v = IntOrString(n)
	return nil
}

// Schedule is a single scheduled trigger entry.
type Schedule struct {
	DaysToRelease           IntOrString `json:"daysToRelease"`
	StartHours              int         `json:"startHours"`
	StartMinutes            int         `json:"startMinutes"`
	TimeZoneID              string      `json:"timeZoneId"`
	ScheduleOnlyWithChanges *bool       `json:"scheduleOnlyWithChanges"`
}

// Trigger is a release definition trigger (schedule, artifact, pull request, etc.).
type Trigger struct {
	TriggerType string     `json:"triggerType"`
	Schedules   []Schedule `json:"schedules"`
	Schedule    *Schedule  `json:"schedule"`
}

// WorkflowTask is a single task/step within a deploy phase.
type WorkflowTask struct {
	Name             string                 `json:"name"`
	Enabled          bool                   `json:"enabled"`
	TaskID           string                 `json:"taskId"`
	Version          string                 `json:"version"`
	Condition        string                 `json:"condition"`
	ContinueOnError  bool                   `json:"continueOnError"`
	AlwaysRun        bool                   `json:"alwaysRun"`
	TimeoutInMinutes int                    `json:"timeoutInMinutes"`
	Environment      map[string]string      `json:"environment"`
	Inputs           map[string]interface{} `json:"inputs"`
}

// ParallelExecution describes how a deploy phase fans out across agents.
type ParallelExecution struct {
	ParallelExecutionType string `json:"parallelExecutionType"`
}

// DeploymentInput holds the agent job settings (pool, demands, timeout, etc.) for a deploy phase.
type DeploymentInput struct {
	QueueID                   int               `json:"queueId"`
	Demands                   []string          `json:"demands"`
	ParallelExecution         ParallelExecution `json:"parallelExecution"`
	TimeoutInMinutes          int               `json:"timeoutInMinutes"`
	JobCancelTimeoutInMinutes int               `json:"jobCancelTimeoutInMinutes"`
	SkipArtifactsDownload     bool              `json:"skipArtifactsDownload"`
	Condition                 string            `json:"condition"`
}

// DeployPhase is a job within a stage (environment).
type DeployPhase struct {
	Name            string           `json:"name"`
	PhaseType       string           `json:"phaseType"`
	WorkflowTasks   []WorkflowTask   `json:"workflowTasks"`
	DeploymentInput *DeploymentInput `json:"deploymentInput"`
}

// ConfigVariable is a release definition variable; Value is empty for secrets.
type ConfigVariable struct {
	Value         string `json:"value"`
	IsSecret      bool   `json:"isSecret"`
	AllowOverride bool   `json:"allowOverride"`
}

// DefinitionEnvironment is a stage within a release definition.
type DefinitionEnvironment struct {
	Name         string                    `json:"name"`
	Variables    map[string]ConfigVariable `json:"variables"`
	DeployPhases []DeployPhase             `json:"deployPhases"`
}

// DefinitionDetail is the full release definition detail, including stages and triggers.
type DefinitionDetail struct {
	ID           int                       `json:"id"`
	Name         string                    `json:"name"`
	Path         string                    `json:"path"`
	Variables    map[string]ConfigVariable `json:"variables"`
	Triggers     []Trigger                 `json:"triggers"`
	Environments []DefinitionEnvironment   `json:"environments"`
}

// ParsePipelinePath splits a "Project\Folder\SubFolder\PipelineName" style path into the
// project, folder path, and pipeline name.
func ParsePipelinePath(pipelinePath string) (project, path, name string, err error) {
	segments := splitNonEmpty(strings.ReplaceAll(pipelinePath, "/", `\`), `\`)
	if len(segments) < 2 {
		return "", "", "", fmt.Errorf("pipeline path must look like 'Project\\PipelineName'")
	}

	project = segments[0]
	name = segments[len(segments)-1]
	folderSegments := segments[1 : len(segments)-1]
	if len(folderSegments) == 0 {
		path = `\`
	} else {
		path = `\` + strings.Join(folderSegments, `\`)
	}

	return project, path, name, nil
}

// FindDefinition finds the release definition matching path+name within a project's definitions.
func (c Config) FindDefinition(project, path, name string) (ReleaseDefinition, error) {
	definitions, err := c.ListReleaseDefinitions(project)
	if err != nil {
		return ReleaseDefinition{}, err
	}
	for _, d := range definitions {
		folder := d.Path
		if folder == "" {
			folder = `\`
		}
		if folder == path && d.Name == name {
			return d, nil
		}
	}
	return ReleaseDefinition{}, fmt.Errorf("pipeline not found: %s%s\\%s", project, path, name)
}

// GetDefinitionDetail fetches full details (stages, tasks, triggers, etc.) for a release definition.
func (c Config) GetDefinitionDetail(project string, definitionID int) (DefinitionDetail, error) {
	url := fmt.Sprintf("%s/%s/%s/_apis/release/definitions/%d?api-version=6.0", c.OrgURL, Collection, project, definitionID)
	var detail DefinitionDetail
	if err := c.Get(url, &detail); err != nil {
		return DefinitionDetail{}, err
	}
	return detail, nil
}

// ResolveDefinition resolves a "Project\Folder\PipelineName" path to its full definition detail.
func (c Config) ResolveDefinition(pipelinePath string) (DefinitionDetail, error) {
	project, path, name, err := ParsePipelinePath(pipelinePath)
	if err != nil {
		return DefinitionDetail{}, err
	}
	definition, err := c.FindDefinition(project, path, name)
	if err != nil {
		return DefinitionDetail{}, err
	}
	return c.GetDefinitionDetail(project, definition.ID)
}

// GetDefinitionDetailRaw fetches a release definition as an untyped map, preserving every field
// so it can be safely modified and PUT back without losing data the typed DefinitionDetail
// struct doesn't model.
func (c Config) GetDefinitionDetailRaw(project string, definitionID int) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/%s/%s/_apis/release/definitions/%d?api-version=6.0", c.OrgURL, Collection, project, definitionID)
	var detail map[string]interface{}
	if err := c.Get(url, &detail); err != nil {
		return nil, err
	}

	return detail, nil
}

// UpdateDefinitionRaw updates a release definition (PUT) from an untyped map, as returned by
// GetDefinitionDetailRaw.
func (c Config) UpdateDefinitionRaw(project string, definition map[string]interface{}, comment string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/%s/%s/_apis/release/definitions?api-version=6.0", c.OrgURL, Collection, project)
	if comment != "" {
		definition["comment"] = comment
	}
	var updated map[string]interface{}
	if err := c.Put(url, definition, &updated); err != nil {
		return nil, err
	}
	return updated, nil
}
