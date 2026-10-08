package detectdeprecatedtasks

import (
	"strings"
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func def(id string, major int, dep, dis bool) azuredevops.TaskDefinition {
	d := azuredevops.TaskDefinition{ID: id, Name: id, Deprecated: dep, Disabled: dis}
	d.Version.Major = major
	return d
}

func TestInspect(t *testing.T) {
	catalog := azuredevops.NewTaskCatalog([]azuredevops.TaskDefinition{
		def("old", 1, true, false), def("off", 1, false, true), def("ok", 1, false, false), def("ok", 2, false, false),
	})
	step := func(id, version string, enabled bool) interface{} {
		return obj{"taskId": id, "name": id, "version": version, "enabled": enabled}
	}
	raw := obj{"environments": []interface{}{obj{"name": "S", "deployPhases": []interface{}{obj{"name": "P", "workflowTasks": []interface{}{
		step("old", "1.*", true), step("off", "1.*", true), step("gone", "1.*", true), step("ok", "9.*", true), step("ok", "1.*", true), step("old", "1.*", false),
	}}}}}}
	var kinds []string
	for _, f := range inspect(raw, catalog, false) {
		kinds = append(kinds, f.kind)
	}
	if got := strings.Join(kinds, ","); got != "DEPRECATED,DISABLED,MISSING,UNSUPPORTED" {
		t.Fatalf("kinds = %s", got)
	}
	if n := len(inspect(raw, catalog, true)); n != 5 {
		t.Fatalf("with outdated: %d findings", n)
	}
}
