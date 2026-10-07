// Package pipelinepolicy loads, audits, and enforces policies for classic release definitions.
package pipelinepolicy

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RetentionPolicy defines minimum retention settings applied to every stage.
type RetentionPolicy struct {
	DaysToKeep     int   `yaml:"days_to_keep"`
	ReleasesToKeep int   `yaml:"releases_to_keep"`
	RetainBuild    *bool `yaml:"retain_build"`
}

// Policy is the supported folder-wide release pipeline policy schema.
type Policy struct {
	RequiredVariables     map[string]string `yaml:"required_variables"`
	ForbiddenVariables    []string          `yaml:"forbidden_variables"`
	RequiredStepTitles    []string          `yaml:"required_step_titles"`
	ForbiddenStepTitles   []string          `yaml:"forbidden_step_titles"`
	RequiredAgentPool     string            `yaml:"required_agent_pool"`
	RequiredDemands       []string          `yaml:"required_demands"`
	MaxJobTimeoutMinutes  int               `yaml:"max_job_timeout_minutes"`
	RequireEnabledSteps   bool              `yaml:"require_enabled_steps"`
	MinimumStageRetention *RetentionPolicy  `yaml:"minimum_stage_retention"`
}

// Result contains all detected violations and the subset fixed in memory.
type Result struct {
	Violations []string
	Changes    []string
}

// Load reads and validates a YAML policy file.
func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read policy: %w", err)
	}
	var policy Policy
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("parse policy YAML: %w", err)
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// Validate checks rule values and rejects empty policies.
func (p Policy) Validate() error {
	if p.MaxJobTimeoutMinutes < 0 {
		return fmt.Errorf("max_job_timeout_minutes cannot be negative")
	}
	if p.MinimumStageRetention != nil && (p.MinimumStageRetention.DaysToKeep < 0 || p.MinimumStageRetention.ReleasesToKeep < 0) {
		return fmt.Errorf("minimum_stage_retention values cannot be negative")
	}
	if len(p.RequiredVariables) == 0 && len(p.ForbiddenVariables) == 0 && len(p.RequiredStepTitles) == 0 &&
		len(p.ForbiddenStepTitles) == 0 && p.RequiredAgentPool == "" && len(p.RequiredDemands) == 0 &&
		p.MaxJobTimeoutMinutes == 0 && !p.RequireEnabledSteps && p.MinimumStageRetention == nil {
		return fmt.Errorf("policy contains no rules")
	}
	return nil
}

// Evaluate audits raw and, when enforce is true, applies every safely enforceable rule.
// requiredQueueID must be resolved by the caller when RequiredAgentPool is configured.
func Evaluate(raw map[string]interface{}, policy Policy, requiredQueueID int, enforce bool) Result {
	var result Result
	add := func(violation, change string, fix func()) {
		result.Violations = append(result.Violations, violation)
		if enforce && fix != nil {
			fix()
			result.Changes = append(result.Changes, change)
		}
	}

	vars, _ := raw["variables"].(map[string]interface{})
	for name, wanted := range policy.RequiredVariables {
		current, exists := vars[name].(map[string]interface{})
		if exists {
			if secret, _ := current["isSecret"].(bool); secret {
				continue // Azure DevOps does not return secret values; existence is all we can verify.
			}
			if value, _ := current["value"].(string); value == wanted {
				continue
			}
		}
		name, wanted := name, wanted
		add(fmt.Sprintf("pipeline variable %q is missing or has the wrong value", name), fmt.Sprintf("set pipeline variable %s", name), func() {
			if vars == nil {
				vars = map[string]interface{}{}
				raw["variables"] = vars
			}
			vars[name] = map[string]interface{}{"value": wanted, "isSecret": false}
		})
	}
	for _, name := range policy.ForbiddenVariables {
		if _, exists := vars[name]; !exists {
			continue
		}
		name := name
		add(fmt.Sprintf("forbidden pipeline variable %q exists", name), fmt.Sprintf("remove pipeline variable %s", name), func() { delete(vars, name) })
	}

	foundRequiredSteps := map[string]bool{}
	for _, title := range policy.RequiredStepTitles {
		foundRequiredSteps[title] = false
	}
	environments, _ := raw["environments"].([]interface{})
	for _, environmentValue := range environments {
		environment, ok := environmentValue.(map[string]interface{})
		if !ok {
			continue
		}
		stageName, _ := environment["name"].(string)
		applyRetention(environment, stageName, policy.MinimumStageRetention, enforce, &result)
		phases, _ := environment["deployPhases"].([]interface{})
		for _, phaseValue := range phases {
			phase, ok := phaseValue.(map[string]interface{})
			if !ok {
				continue
			}
			jobName, _ := phase["name"].(string)
			label := stageName + " / " + jobName
			applyJobPolicy(phase, label, policy, requiredQueueID, enforce, &result)
			tasks, _ := phase["workflowTasks"].([]interface{})
			kept := make([]interface{}, 0, len(tasks))
			for _, taskValue := range tasks {
				task, ok := taskValue.(map[string]interface{})
				if !ok {
					kept = append(kept, taskValue)
					continue
				}
				title, _ := task["name"].(string)
				if _, wanted := foundRequiredSteps[title]; wanted {
					foundRequiredSteps[title] = true
				}
				if contains(policy.ForbiddenStepTitles, title) {
					result.Violations = append(result.Violations, fmt.Sprintf("[%s] forbidden step %q exists", label, title))
					if enforce {
						result.Changes = append(result.Changes, fmt.Sprintf("[%s] remove forbidden step %q", label, title))
						continue
					}
				}
				if policy.RequireEnabledSteps {
					if enabled, _ := task["enabled"].(bool); !enabled {
						result.Violations = append(result.Violations, fmt.Sprintf("[%s] step %q is disabled", label, title))
						if enforce {
							task["enabled"] = true
							result.Changes = append(result.Changes, fmt.Sprintf("[%s] enable step %q", label, title))
						}
					}
				}
				kept = append(kept, taskValue)
			}
			if enforce && len(kept) != len(tasks) {
				phase["workflowTasks"] = kept
			}
		}
	}
	for title, found := range foundRequiredSteps {
		if !found {
			result.Violations = append(result.Violations, fmt.Sprintf("required step %q is missing (audit-only; task definition is not specified)", title))
		}
	}
	sort.Strings(result.Violations)
	sort.Strings(result.Changes)
	return result
}

func applyJobPolicy(phase map[string]interface{}, label string, policy Policy, queueID int, enforce bool, result *Result) {
	input, _ := phase["deploymentInput"].(map[string]interface{})
	if input == nil {
		return
	}
	if policy.RequiredAgentPool != "" && intValue(input["queueId"]) != queueID {
		result.Violations = append(result.Violations, fmt.Sprintf("[%s] agent pool is not %q", label, policy.RequiredAgentPool))
		if enforce {
			input["queueId"] = queueID
			result.Changes = append(result.Changes, fmt.Sprintf("[%s] set agent pool to %s", label, policy.RequiredAgentPool))
		}
	}
	demands := stringSlice(input["demands"])
	for _, required := range policy.RequiredDemands {
		if containsFold(demands, required) {
			continue
		}
		result.Violations = append(result.Violations, fmt.Sprintf("[%s] required demand %q is missing", label, required))
		if enforce {
			demands = append(demands, required)
			input["demands"] = stringInterfaces(demands)
			result.Changes = append(result.Changes, fmt.Sprintf("[%s] add demand %s", label, required))
		}
	}
	if policy.MaxJobTimeoutMinutes > 0 {
		current := intValue(input["timeoutInMinutes"])
		if current == 0 || current > policy.MaxJobTimeoutMinutes {
			result.Violations = append(result.Violations, fmt.Sprintf("[%s] timeout %d exceeds maximum %d", label, current, policy.MaxJobTimeoutMinutes))
			if enforce {
				input["timeoutInMinutes"] = policy.MaxJobTimeoutMinutes
				result.Changes = append(result.Changes, fmt.Sprintf("[%s] set timeout to %d minutes", label, policy.MaxJobTimeoutMinutes))
			}
		}
	}
}

func applyRetention(environment map[string]interface{}, stage string, minimum *RetentionPolicy, enforce bool, result *Result) {
	if minimum == nil {
		return
	}
	retention, _ := environment["retentionPolicy"].(map[string]interface{})
	if retention == nil {
		retention = map[string]interface{}{}
	}
	check := func(key, label string, wanted int) {
		if wanted == 0 || intValue(retention[key]) >= wanted {
			return
		}
		result.Violations = append(result.Violations, fmt.Sprintf("[stage %s] retention %s is below %d", stage, label, wanted))
		if enforce {
			retention[key] = wanted
			environment["retentionPolicy"] = retention
			result.Changes = append(result.Changes, fmt.Sprintf("[stage %s] set retention %s to %d", stage, label, wanted))
		}
	}
	check("daysToKeep", "days", minimum.DaysToKeep)
	check("releasesToKeep", "release count", minimum.ReleasesToKeep)
	if minimum.RetainBuild != nil {
		current, _ := retention["retainBuild"].(bool)
		if current != *minimum.RetainBuild {
			result.Violations = append(result.Violations, fmt.Sprintf("[stage %s] retain build must be %v", stage, *minimum.RetainBuild))
			if enforce {
				retention["retainBuild"] = *minimum.RetainBuild
				environment["retentionPolicy"] = retention
				result.Changes = append(result.Changes, fmt.Sprintf("[stage %s] set retain build to %v", stage, *minimum.RetainBuild))
			}
		}
	}
}

func intValue(value interface{}) int {
	switch value := value.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func stringSlice(value interface{}) []string {
	items, _ := value.([]interface{})
	if direct, ok := value.([]string); ok {
		return append([]string(nil), direct...)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func stringInterfaces(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}
