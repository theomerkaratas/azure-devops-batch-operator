package replacepipelinecontent

import (
	"regexp"
	"testing"
)

func TestReplaceContentInSelectedFields(t *testing.T) {
	task := map[string]interface{}{"name": "Deploy Service-API", "inputs": map[string]interface{}{
		"inlineScript": "restart Service-API\nrestart Service-Web", "workingDirectory": "Service-API",
	}}
	stageVar := map[string]interface{}{"value": "https://Service-API.example"}
	raw := map[string]interface{}{
		"variables": map[string]interface{}{"Target": map[string]interface{}{"value": "Service-API"}},
		"environments": []interface{}{map[string]interface{}{
			"name": "Production", "variables": map[string]interface{}{"Url": stageVar},
			"deployPhases": []interface{}{map[string]interface{}{"name": "Agent job", "workflowTasks": []interface{}{task}}},
		}},
	}

	changes := replaceContent(raw, regexp.MustCompile(`Service-(\w+)`), "App-$1", targets{true, true, true})
	if len(changes) != 4 {
		t.Fatalf("changes = %d, want 4: %#v", len(changes), changes)
	}
	if got := task["name"]; got != "Deploy App-API" {
		t.Errorf("title = %q, want Deploy App-API", got)
	}
	inputs := task["inputs"].(map[string]interface{})
	if got := inputs["inlineScript"]; got != "restart App-API\nrestart App-Web" {
		t.Errorf("script = %q", got)
	}
	if got := inputs["workingDirectory"]; got != "Service-API" {
		t.Errorf("non-script input changed to %q", got)
	}
	if got := stageVar["value"]; got != "https://App-API.example" {
		t.Errorf("stage variable = %q", got)
	}
}

func TestReplaceContentHonorsFieldSelection(t *testing.T) {
	task := map[string]interface{}{"name": "old", "inputs": map[string]interface{}{"script": "old"}}
	raw := map[string]interface{}{"variables": map[string]interface{}{"V": map[string]interface{}{"value": "old"}}, "environments": []interface{}{map[string]interface{}{
		"name": "Stage", "deployPhases": []interface{}{map[string]interface{}{"name": "Job", "workflowTasks": []interface{}{task}}},
	}}}
	replaceContent(raw, regexp.MustCompile("old"), "new", targets{titles: true})
	if task["name"] != "new" || task["inputs"].(map[string]interface{})["script"] != "old" {
		t.Fatalf("field selection not honored: %#v", task)
	}
	if raw["variables"].(map[string]interface{})["V"].(map[string]interface{})["value"] != "old" {
		t.Fatal("variable changed when variables were not selected")
	}
}

func TestParseTargetsRejectsUnknownField(t *testing.T) {
	if _, err := parseTargets("scripts,unknown"); err == nil {
		t.Fatal("expected unknown field error")
	}
}
