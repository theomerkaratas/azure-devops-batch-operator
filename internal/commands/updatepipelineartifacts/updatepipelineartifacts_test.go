package updatepipelineartifacts

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func raw() obj {
	return obj{
		"artifacts": []interface{}{obj{"type": "Build", "alias": "_Old", "sourceId": "p1:5", "definitionReference": obj{
			"definition": obj{"id": "5", "name": "Old"}, "project": obj{"id": "p1", "name": "Proj"}}}},
		"environments": []interface{}{obj{"deployPhases": []interface{}{obj{"workflowTasks": []interface{}{
			obj{"inputs": obj{"script": "cp $(System.DefaultWorkingDirectory)/_Old/drop $(Release.Artifacts._Old.BuildNumber)"}}}}}}},
	}
}

func fake(project, name string) (azuredevops.BuildDefinitionRef, error) {
	return azuredevops.BuildDefinitionRef{ID: 9, Name: name, ProjectID: "p1", ProjectName: project}, nil
}

func TestReplaceSourceAndAlias(t *testing.T) {
	r := raw()
	lines, err := apply(r, options{matchDefinition: "old", setDefinition: "New", setAlias: "_New", setBranch: "refs/heads/main", rewrite: true}, fake)
	if err != nil || len(lines) != 2 {
		t.Fatalf("%v %v", lines, err)
	}
	a := r["artifacts"].([]interface{})[0].(obj)
	if a["alias"] != "_New" || a["sourceId"] != "p1:9" {
		t.Fatalf("%v", a)
	}
	script := r["environments"].([]interface{})[0].(obj)["deployPhases"].([]interface{})[0].(obj)["workflowTasks"].([]interface{})[0].(obj)["inputs"].(obj)["script"]
	if script != "cp $(System.DefaultWorkingDirectory)/_New/drop $(Release.Artifacts._New.BuildNumber)" {
		t.Fatalf("%v", script)
	}
	if lines, _ := apply(r, options{matchAlias: "_New", setAlias: "_New"}, fake); lines != nil {
		t.Fatal("not idempotent")
	}
}

func TestValidateOptions(t *testing.T) {
	if (options{setAlias: "x"}).validate() == nil || (options{matchAlias: "x"}).validate() == nil {
		t.Fatal("matcher and setter are required")
	}
}
