package createpowershellpipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTasksPreservesFileAndInlineOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepare.ps1")
	if err := os.WriteFile(path, []byte("Write-Host 'prepare'"), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := loadTasks([]taskSource{
		{kind: "inline", value: "Write-Host 'first'"},
		{kind: "file", value: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("task count = %d, want 2", len(tasks))
	}
	if tasks[0].content != "Write-Host 'first'" || tasks[1].name != "prepare" {
		t.Fatalf("tasks are out of order: %#v", tasks)
	}
}

func TestBuildDefinitionCreatesPowerShellTasks(t *testing.T) {
	variables, err := parseVariables([]string{"Environment=Production", "Empty="})
	if err != nil {
		t.Fatal(err)
	}
	definition := buildDefinition("Restart", `\Deploy`, "description", "Production", 42, true, []scriptTask{
		{name: "stop", content: "Stop-Service Example"},
		{name: "start", content: "Start-Service Example"},
	}, variables)
	if definition["name"] != "Restart" || definition["path"] != `\Deploy` {
		t.Fatalf("unexpected definition identity: %#v", definition)
	}
	gotVariables := definition["variables"].(map[string]interface{})
	if len(gotVariables) != 2 {
		t.Fatalf("variable count = %d, want 2", len(gotVariables))
	}
	environments := definition["environments"].([]map[string]interface{})
	if len(environments) != 1 || environments[0]["name"] != "Production" {
		t.Fatalf("unexpected environments: %#v", environments)
	}
	phases := environments[0]["deployPhases"].([]map[string]interface{})
	input := phases[0]["deploymentInput"].(map[string]interface{})
	if input["queueId"] != 42 {
		t.Fatalf("queueId = %#v, want 42", input["queueId"])
	}
	tasks := phases[0]["workflowTasks"].([]map[string]interface{})
	if len(tasks) != 2 || tasks[0]["taskId"] != powerShellTaskID {
		t.Fatalf("unexpected workflow tasks: %#v", tasks)
	}
	inputs := tasks[0]["inputs"].(map[string]interface{})
	if inputs["script"] != "Stop-Service Example" || inputs["pwsh"] != "true" {
		t.Fatalf("unexpected task inputs: %#v", inputs)
	}
}

func TestParseVariablesRejectsInvalidAndDuplicateNames(t *testing.T) {
	for _, values := range [][]string{{"missing-separator"}, {"=value"}, {"A=1", "A=2"}} {
		if _, err := parseVariables(values); err == nil {
			t.Errorf("parseVariables(%q) succeeded, want error", values)
		}
	}
}
