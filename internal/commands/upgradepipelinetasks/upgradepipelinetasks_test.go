package upgradepipelinetasks

import (
	"strings"
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func def(major, minor int, inputs ...azuredevops.TaskInput) azuredevops.TaskDefinition {
	d := azuredevops.TaskDefinition{ID: "AAAA", Name: "PowerShell", FriendlyName: "PowerShell", Inputs: inputs}
	d.Version.Major, d.Version.Minor = major, minor
	return d
}

func pipeline(version string, inputs obj) obj {
	return obj{"environments": []interface{}{obj{"name": "Prod", "deployPhases": []interface{}{obj{"name": "Run", "workflowTasks": []interface{}{
		obj{"taskId": "aaaa", "name": "Step", "version": version, "inputs": inputs}}}}}}}
}

func TestUpgradeCompatibility(t *testing.T) {
	catalog := azuredevops.NewTaskCatalog([]azuredevops.TaskDefinition{
		def(1, 0, azuredevops.TaskInput{Name: "script"}, azuredevops.TaskInput{Name: "legacy"}),
		def(2, 3, azuredevops.TaskInput{Name: "script"}, azuredevops.TaskInput{Name: "target", Required: true}),
	})
	raw := pipeline("1.*", obj{"script": "x", "legacy": "y"})
	lines, warnings := upgrade(raw, catalog, "aaaa", 0, 2, false)
	if len(lines) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], `"legacy"`) || !strings.Contains(warnings[0], `"target"`) {
		t.Fatalf("%v %v", lines, warnings)
	}
	lines, _ = upgrade(raw, catalog, "aaaa", 0, 2, true)
	if len(lines) != 1 {
		t.Fatalf("%v", lines)
	}
	step := raw["environments"].([]interface{})[0].(obj)["deployPhases"].([]interface{})[0].(obj)["workflowTasks"].([]interface{})[0].(obj)
	if step["version"] != "2.*" {
		t.Fatalf("version %v", step["version"])
	}
	if lines, _ := upgrade(raw, catalog, "aaaa", 0, 2, false); lines != nil {
		t.Fatal("not idempotent")
	}
	if lines, w := upgrade(raw, catalog, "aaaa", 0, 1, false); lines != nil || len(w) != 1 {
		t.Fatal("downgrade must need force")
	}
}

func TestMissingTargetVersion(t *testing.T) {
	catalog := azuredevops.NewTaskCatalog([]azuredevops.TaskDefinition{def(1, 0)})
	if lines, w := upgrade(pipeline("1.*", obj{}), catalog, "aaaa", 0, 5, false); lines != nil || len(w) != 1 {
		t.Fatalf("%v %v", lines, w)
	}
}
