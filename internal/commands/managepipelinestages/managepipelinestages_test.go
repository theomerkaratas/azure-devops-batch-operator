package managepipelinestages

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/stageutil"
)

func pipeline() map[string]interface{} {
	raw := map[string]interface{}{}
	mk := func(n string, deps ...string) stageutil.Map {
		s := stageutil.Map{"name": n}
		stageutil.SetDependencies(s, deps)
		return s
	}
	stageutil.Save(raw, []stageutil.Map{mk("Dev"), mk("QA", "Dev"), mk("Prod", "QA")})
	return raw
}

func names(raw map[string]interface{}) []string {
	var out []string
	for _, s := range stageutil.Stages(raw) {
		out = append(out, stageutil.Name(s))
	}
	return out
}

func TestAddRenameRemove(t *testing.T) {
	raw := pipeline()
	if _, err := apply(raw, options{action: "add", stage: "Perf", after: "QA"}); err != nil {
		t.Fatal(err)
	}
	if got := names(raw); got[2] != "Perf" || len(got) != 4 {
		t.Fatalf("%v", got)
	}
	if _, err := apply(raw, options{action: "rename", stage: "QA", newName: "Test"}); err != nil {
		t.Fatal(err)
	}
	if d := stageutil.Dependencies(stageutil.Stages(raw)[3]); d[0] != "Test" {
		t.Fatalf("deps %v", d)
	}
	if _, err := apply(raw, options{action: "remove", stage: "Test"}); err == nil {
		t.Fatal("remove with dependents must fail")
	}
	if _, err := apply(raw, options{action: "remove", stage: "Test", rewire: true}); err != nil {
		t.Fatal(err)
	}
	if d := stageutil.Dependencies(stageutil.Stages(raw)[2]); d[0] != "Dev" {
		t.Fatalf("deps %v", d)
	}
}

func TestCloneAndReorderRules(t *testing.T) {
	raw := pipeline()
	if _, err := apply(raw, options{action: "clone", stage: "QA", newName: "QA2"}); err != nil {
		t.Fatal(err)
	}
	if got := names(raw); got[2] != "QA2" {
		t.Fatalf("%v", got)
	}
	if _, err := apply(raw, options{action: "reorder", stage: "Prod", position: 1}); err == nil {
		t.Fatal("moving a stage before its dependency must fail")
	}
	if names(raw)[3] != "Prod" {
		t.Fatal("failed reorder modified the definition")
	}
}
