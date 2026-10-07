package updatepipelinestagetriggers

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
	stageutil.Save(raw, []stageutil.Map{mk("Dev"), mk("QA"), mk("Prod")})
	return raw
}

func TestSequentialManualAndIdempotence(t *testing.T) {
	raw := pipeline()
	lines, err := apply(raw, "sequential", nil, nil)
	if err != nil || len(lines) != 2 {
		t.Fatalf("%v %v", lines, err)
	}
	if lines, _ := apply(raw, "sequential", nil, nil); lines != nil {
		t.Fatalf("not idempotent: %v", lines)
	}
	if _, err := apply(raw, "manual", []string{"Prod"}, nil); err != nil {
		t.Fatal(err)
	}
	if k, _ := stageutil.TriggerOf(stageutil.Stages(raw)[2]); k != stageutil.TriggerManual {
		t.Fatalf("kind %s", k)
	}
}

func TestAfterStagesValidation(t *testing.T) {
	raw := pipeline()
	if _, err := apply(raw, "after-stages", []string{"Dev"}, []string{"Prod"}); err == nil {
		t.Fatal("dependency on a later stage must fail")
	}
	raw = pipeline()
	if _, err := apply(raw, "after-stages", []string{"Prod"}, []string{"Nope"}); err == nil {
		t.Fatal("missing stage must fail")
	}
	lines, err := apply(raw, "after-stages", []string{"Prod"}, []string{"Dev", "QA"})
	if err != nil || len(lines) != 1 {
		t.Fatalf("%v %v", lines, err)
	}
}
