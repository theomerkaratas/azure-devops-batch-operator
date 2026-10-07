package updatepipelinevariablegroups

import "testing"

func TestApplyVariableGroupsAtAllScopes(t *testing.T) {
	raw := map[string]interface{}{"variableGroups": []interface{}{1.0, 2.0}, "environments": []interface{}{map[string]interface{}{"name": "Prod", "variableGroups": []interface{}{2.0, 3.0}}}}
	changes := apply(raw, "all", "Prod", []int{4, 4}, []int{2})
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	if got, want := groupIDs(raw["variableGroups"]), []int{1, 4}; !equal(got, want) {
		t.Fatalf("pipeline groups = %v, want %v", got, want)
	}
	env := raw["environments"].([]interface{})[0].(map[string]interface{})
	if got, want := groupIDs(env["variableGroups"]), []int{3, 4}; !equal(got, want) {
		t.Fatalf("stage groups = %v, want %v", got, want)
	}
}

func TestApplyVariableGroupsNoOp(t *testing.T) {
	raw := map[string]interface{}{"variableGroups": []interface{}{1.0}}
	if changes := apply(raw, "pipeline", "", []int{1}, []int{9}); len(changes) != 0 {
		t.Fatalf("unexpected changes: %#v", changes)
	}
}
