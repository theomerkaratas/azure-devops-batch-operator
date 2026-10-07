package copypipelinestage

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/stageutil"
)

func src() stageutil.Map {
	s := stageutil.Map{"id": 5.0, "name": "Prod", "variables": stageutil.Map{"S": stageutil.Map{"isSecret": true, "value": nil}}}
	stageutil.SetDependencies(s, []string{"Missing"})
	return s
}

func TestCopyModes(t *testing.T) {
	raw := map[string]interface{}{}
	dev := stageutil.Map{"name": "Dev"}
	stageutil.SetDependencies(dev, nil)
	stageutil.Save(raw, []stageutil.Map{dev})

	lines, err := copyStage(raw, src(), options{stage: "Prod", onExisting: "skip"})
	if err != nil || len(lines) != 1 {
		t.Fatalf("add: %v %v", lines, err)
	}
	prod := stageutil.Stages(raw)[1]
	if _, has := prod["id"]; has || len(stageutil.Dependencies(prod)) != 0 {
		t.Fatalf("id/deps not cleaned: %v", prod)
	}
	if lines, _ := copyStage(raw, src(), options{stage: "Prod", onExisting: "skip"}); lines != nil {
		t.Fatal("skip must not change")
	}
	if lines, err := copyStage(raw, src(), options{stage: "Prod", onExisting: "rename"}); err != nil || len(lines) != 1 || stageutil.Index(stageutil.Stages(raw), "Prod-copy") < 0 {
		t.Fatalf("rename: %v %v", lines, err)
	}
	if lines, err := copyStage(raw, src(), options{stage: "Prod", onExisting: "replace"}); err != nil || len(lines) != 1 {
		t.Fatalf("replace: %v %v", lines, err)
	}
}
