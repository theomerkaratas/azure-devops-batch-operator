package deletepipelinesteps

import "testing"

func TestRemoveStepsByExactTitle(t *testing.T) {
	wanted := map[string]interface{}{"name": "Remove me"}
	differentCase := map[string]interface{}{"name": "remove me"}
	other := map[string]interface{}{"name": "Keep me"}
	phase := map[string]interface{}{
		"name":          "Agent job",
		"workflowTasks": []interface{}{wanted, differentCase, other, map[string]interface{}{"name": "Remove me"}},
	}
	raw := map[string]interface{}{"environments": []interface{}{map[string]interface{}{
		"name":         "Production",
		"deployPhases": []interface{}{phase},
	}}}

	changes := removeStepsByExactTitle(raw, "Remove me")
	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(changes))
	}
	tasks := phase["workflowTasks"].([]interface{})
	if len(tasks) != 2 || tasks[0].(map[string]interface{})["name"] != "remove me" || tasks[1].(map[string]interface{})["name"] != "Keep me" {
		t.Fatalf("remaining tasks = %#v, want differently-cased and unrelated tasks", tasks)
	}
}

func TestRemoveStepsByExactTitleLeavesMissingMatchUnchanged(t *testing.T) {
	tasks := []interface{}{map[string]interface{}{"name": "Keep me"}}
	phase := map[string]interface{}{"name": "Job", "workflowTasks": tasks}
	raw := map[string]interface{}{"environments": []interface{}{map[string]interface{}{
		"name": "Stage", "deployPhases": []interface{}{phase},
	}}}

	if changes := removeStepsByExactTitle(raw, "Missing"); len(changes) != 0 {
		t.Fatalf("changes = %#v, want none", changes)
	}
	if got := len(phase["workflowTasks"].([]interface{})); got != 1 {
		t.Fatalf("remaining tasks = %d, want 1", got)
	}
}
