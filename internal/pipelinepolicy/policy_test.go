package pipelinepolicy

import "testing"

func boolPtr(value bool) *bool { return &value }

func sampleDefinition() map[string]interface{} {
	return map[string]interface{}{
		"variables": map[string]interface{}{
			"Environment": map[string]interface{}{"value": "dev", "isSecret": false},
			"Obsolete":    map[string]interface{}{"value": "yes", "isSecret": false},
		},
		"environments": []interface{}{map[string]interface{}{
			"name":            "Production",
			"retentionPolicy": map[string]interface{}{"daysToKeep": float64(5), "releasesToKeep": float64(1), "retainBuild": false},
			"deployPhases": []interface{}{map[string]interface{}{
				"name": "Agent job",
				"deploymentInput": map[string]interface{}{
					"queueId": float64(1), "demands": []interface{}{"Agent.OS -equals Windows_NT"}, "timeoutInMinutes": float64(120),
				},
				"workflowTasks": []interface{}{
					map[string]interface{}{"name": "Deploy", "enabled": false},
					map[string]interface{}{"name": "Deprecated", "enabled": true},
				},
			}},
		}},
	}
}

func TestEvaluateAuditsWithoutMutation(t *testing.T) {
	raw := sampleDefinition()
	policy := Policy{
		RequiredVariables:     map[string]string{"Environment": "prod", "Owner": "platform"},
		ForbiddenVariables:    []string{"Obsolete"},
		RequiredStepTitles:    []string{"Deploy", "Health check"},
		ForbiddenStepTitles:   []string{"Deprecated"},
		RequiredAgentPool:     "Production",
		RequiredDemands:       []string{"region -equals eu"},
		MaxJobTimeoutMinutes:  60,
		RequireEnabledSteps:   true,
		MinimumStageRetention: &RetentionPolicy{DaysToKeep: 30, ReleasesToKeep: 3, RetainBuild: boolPtr(true)},
	}
	result := Evaluate(raw, policy, 42, false)
	if len(result.Violations) != 12 {
		t.Fatalf("violations = %d, want 12: %#v", len(result.Violations), result.Violations)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("audit produced changes: %#v", result.Changes)
	}
	vars := raw["variables"].(map[string]interface{})
	if vars["Environment"].(map[string]interface{})["value"] != "dev" {
		t.Fatal("audit mutated variable")
	}
}

func TestEvaluateEnforcesFixableRules(t *testing.T) {
	raw := sampleDefinition()
	policy := Policy{
		RequiredVariables:     map[string]string{"Environment": "prod", "Owner": "platform"},
		ForbiddenVariables:    []string{"Obsolete"},
		RequiredStepTitles:    []string{"Health check"},
		ForbiddenStepTitles:   []string{"Deprecated"},
		RequiredAgentPool:     "Production",
		RequiredDemands:       []string{"region -equals eu"},
		MaxJobTimeoutMinutes:  60,
		RequireEnabledSteps:   true,
		MinimumStageRetention: &RetentionPolicy{DaysToKeep: 30, ReleasesToKeep: 3, RetainBuild: boolPtr(true)},
	}
	result := Evaluate(raw, policy, 42, true)
	if len(result.Changes) != 11 {
		t.Fatalf("changes = %d, want 11: %#v", len(result.Changes), result.Changes)
	}
	vars := raw["variables"].(map[string]interface{})
	if vars["Environment"].(map[string]interface{})["value"] != "prod" || vars["Owner"] == nil || vars["Obsolete"] != nil {
		t.Fatalf("variables not enforced: %#v", vars)
	}
	env := raw["environments"].([]interface{})[0].(map[string]interface{})
	phase := env["deployPhases"].([]interface{})[0].(map[string]interface{})
	input := phase["deploymentInput"].(map[string]interface{})
	if intValue(input["queueId"]) != 42 || intValue(input["timeoutInMinutes"]) != 60 {
		t.Fatalf("job policy not enforced: %#v", input)
	}
	tasks := phase["workflowTasks"].([]interface{})
	if len(tasks) != 1 || tasks[0].(map[string]interface{})["enabled"] != true {
		t.Fatalf("tasks not enforced: %#v", tasks)
	}
}

func TestSecretRequiredVariableIsExistenceOnly(t *testing.T) {
	raw := map[string]interface{}{"variables": map[string]interface{}{
		"Password": map[string]interface{}{"value": "", "isSecret": true},
	}}
	result := Evaluate(raw, Policy{RequiredVariables: map[string]string{"Password": "cannot-read"}}, 0, true)
	if len(result.Violations) != 0 || len(result.Changes) != 0 {
		t.Fatalf("secret variable should be accepted by existence: %#v", result)
	}
}

func TestValidateRejectsEmptyPolicy(t *testing.T) {
	if err := (Policy{}).Validate(); err == nil {
		t.Fatal("expected empty policy error")
	}
}
