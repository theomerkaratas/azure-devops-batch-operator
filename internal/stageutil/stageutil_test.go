package stageutil

import "testing"

func stage(name string, deps ...string) Map {
	s := Map{"name": name}
	SetDependencies(s, deps)
	return s
}

func TestValidateOrderAndSave(t *testing.T) {
	stages := []Map{stage("Dev"), stage("QA", "Dev"), stage("Prod", "QA")}
	if err := Validate(stages); err != nil {
		t.Fatal(err)
	}
	if Validate([]Map{stage("QA", "Dev"), stage("Dev")}) == nil {
		t.Fatal("dependency after dependent must fail")
	}
	if Validate([]Map{stage("A", "Missing")}) == nil {
		t.Fatal("missing dependency must fail")
	}
	raw := Map{}
	Save(raw, stages)
	if Stages(raw)[2]["rank"] != 3.0 {
		t.Fatal("ranks not renumbered")
	}
}

func TestRenameAndPrune(t *testing.T) {
	stages := []Map{stage("Dev"), stage("QA", "Dev")}
	RenameDependency(stages, "Dev", "Build")
	if d := Dependencies(stages[1]); len(d) != 1 || d[0] != "Build" {
		t.Fatalf("deps = %v", d)
	}
	PruneDependencies(stages[1], map[string]bool{"qa": true})
	if len(Dependencies(stages[1])) != 0 {
		t.Fatal("unknown dependency kept")
	}
}

func TestRebindIDs(t *testing.T) {
	src := Map{"id": 1.0, "deployPhases": []interface{}{Map{"id": 2.0, "name": "J"}}}
	dest := Map{"id": 9.0, "deployPhases": []interface{}{Map{"id": 8.0, "name": "J"}}}
	RebindIDs(src, dest)
	if src["id"] != 9.0 || src["deployPhases"].([]interface{})[0].(Map)["id"] != 8.0 {
		t.Fatalf("%v", src)
	}
}
