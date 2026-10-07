package synchronizepipelines

import "testing"

func TestSynchronizeSelectedComponents(t *testing.T) {
	reference := map[string]interface{}{"variables": map[string]interface{}{"Plain": map[string]interface{}{"value": "gold"}, "Secret": map[string]interface{}{"value": nil, "isSecret": true}}, "environments": []interface{}{map[string]interface{}{"name": "Prod", "preDeployApprovals": map[string]interface{}{"approvals": []interface{}{1.0}}, "deployPhases": []interface{}{map[string]interface{}{"name": "Deploy", "workflowTasks": []interface{}{map[string]interface{}{"name": "Golden step"}}, "deploymentInput": map[string]interface{}{"queueId": 42.0}}}}}}
	target := map[string]interface{}{"variables": map[string]interface{}{"Plain": map[string]interface{}{"value": "old"}, "Extra": map[string]interface{}{"value": "remove"}, "Secret": map[string]interface{}{"value": "masked-target", "isSecret": true}}, "environments": []interface{}{map[string]interface{}{"name": "Prod", "preDeployApprovals": map[string]interface{}{}, "deployPhases": []interface{}{map[string]interface{}{"name": "Deploy", "workflowTasks": []interface{}{map[string]interface{}{"name": "Old step"}}, "deploymentInput": map[string]interface{}{"queueId": 1.0}}}}}}
	changes := synchronize(target, reference, components{"variables": true, "steps": true, "approvals": true})
	if len(changes) != 3 {
		t.Fatalf("changes = %#v", changes)
	}
	vars := target["variables"].(map[string]interface{})
	if len(vars) != 2 || vars["Secret"].(map[string]interface{})["value"] != "masked-target" {
		t.Fatalf("secret was not preserved: %#v", vars)
	}
	stage := target["environments"].([]interface{})[0].(map[string]interface{})
	job := stage["deployPhases"].([]interface{})[0].(map[string]interface{})
	if job["workflowTasks"].([]interface{})[0].(map[string]interface{})["name"] != "Golden step" {
		t.Fatal("steps not synchronized")
	}
	if job["deploymentInput"].(map[string]interface{})["queueId"] != 1.0 {
		t.Fatal("unselected agent settings changed")
	}
}

func TestReboundStagesPreservesMatchingDestinationIDs(t *testing.T) {
	source := []interface{}{map[string]interface{}{"id": 10.0, "name": "Prod", "deployPhases": []interface{}{map[string]interface{}{"id": 11.0, "name": "Deploy"}}}, map[string]interface{}{"id": 20.0, "name": "New"}}
	destination := []interface{}{map[string]interface{}{"id": 100.0, "name": "Prod", "deployPhases": []interface{}{map[string]interface{}{"id": 101.0, "name": "Deploy"}}}}
	got := reboundStages(source, destination).([]interface{})
	prod := got[0].(map[string]interface{})
	if prod["id"] != 100.0 || prod["deployPhases"].([]interface{})[0].(map[string]interface{})["id"] != 101.0 {
		t.Fatalf("destination IDs not preserved: %#v", prod)
	}
	if _, exists := got[1].(map[string]interface{})["id"]; exists {
		t.Fatal("source ID retained on new stage")
	}
}

func TestParseComponentsRejectsUnknown(t *testing.T) {
	if _, err := parseComponents("steps,magic"); err == nil {
		t.Fatal("expected error")
	}
}
