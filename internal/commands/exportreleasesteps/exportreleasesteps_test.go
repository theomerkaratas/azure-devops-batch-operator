package exportreleasesteps

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func sampleDefinition() map[string]interface{} {
	return map[string]interface{}{
		"name": "Web/Deploy",
		"environments": []interface{}{
			map[string]interface{}{
				"name": "Prod",
				"deployPhases": []interface{}{
					map[string]interface{}{
						"workflowTasks": []interface{}{
							map[string]interface{}{
								"name":   "Install",
								"taskId": powershellTaskID,
								"inputs": map[string]interface{}{"targetType": "inline", "script": "Write-Host 'a'"},
							},
							map[string]interface{}{
								"name":   "Install",
								"taskId": powershellTaskID,
								"inputs": map[string]interface{}{"targetType": "filePath", "scriptPath": "x.ps1"},
							},
							map[string]interface{}{
								"name":   "Run cmd",
								"taskId": commandLineTaskID,
								"inputs": map[string]interface{}{"script": "echo hi\n"},
							},
							map[string]interface{}{
								"name":   "Copy files",
								"taskId": "5bfb729a-a7c8-4a78-a7c3-8d717bb7c13c",
								"inputs": map[string]interface{}{},
							},
						},
					},
				},
			},
		},
	}
}

func TestCollectScriptsSkipsFileBasedPowerShellAndDeduplicates(t *testing.T) {
	scripts, skipped, err := collectScripts(sampleDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("skipped=%d want 1", skipped)
	}
	if len(scripts) != 2 {
		t.Fatalf("got %d scripts, want 2", len(scripts))
	}
	if scripts[0].name != "Install" || scripts[0].ext != ".ps1" {
		t.Errorf("first script %s%s", scripts[0].name, scripts[0].ext)
	}
	if scripts[1].name != "Run cmd" || scripts[1].ext != ".cmd" {
		t.Errorf("second script %s%s", scripts[1].name, scripts[1].ext)
	}
	if !bytes.HasPrefix(scripts[0].data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("ps1 output lacks UTF-8 BOM")
	}
	if !bytes.Contains(scripts[1].data, []byte("\r\n")) {
		t.Error("cmd output lacks CRLF")
	}
}

func TestCollectScriptsUniqueNamesWithinPipeline(t *testing.T) {
	raw := map[string]interface{}{
		"environments": []interface{}{
			map[string]interface{}{"deployPhases": []interface{}{
				map[string]interface{}{"workflowTasks": []interface{}{
					map[string]interface{}{"name": "Step", "taskId": commandLineTaskID, "inputs": map[string]interface{}{"script": "1"}},
					map[string]interface{}{"name": "step", "taskId": commandLineTaskID, "inputs": map[string]interface{}{"script": "2"}},
					map[string]interface{}{"name": "", "taskId": commandLineTaskID, "inputs": map[string]interface{}{"script": "3"}},
				}},
			}},
		},
	}
	scripts, _, err := collectScripts(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Step", "step_2", "step_3"}
	for i, s := range scripts {
		if s.name != want[i] {
			t.Errorf("script %d named %q, want %q", i, s.name, want[i])
		}
	}
}

func TestCollectScriptsReportsUnencodableCharacter(t *testing.T) {
	raw := map[string]interface{}{
		"environments": []interface{}{
			map[string]interface{}{"deployPhases": []interface{}{
				map[string]interface{}{"workflowTasks": []interface{}{
					map[string]interface{}{"name": "Emoji", "taskId": commandLineTaskID, "inputs": map[string]interface{}{"script": "echo 😀"}},
				}},
			}},
		},
	}
	if _, _, err := collectScripts(raw); err == nil || !strings.Contains(err.Error(), "Emoji") {
		t.Errorf("expected error naming the step, got %v", err)
	}
}

func TestCountSteps(t *testing.T) {
	if got := countSteps(sampleDefinition()); got != 4 {
		t.Errorf("countSteps=%d want 4", got)
	}
}

func TestMarshalJSONKeepsScriptsReadable(t *testing.T) {
	data, err := marshalJSON(map[string]interface{}{"script": "if a < b && c > d"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"script\": \"if a < b && c > d\"\n}"
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
	if bytes.HasSuffix(data, []byte("\n")) {
		t.Error("output has a trailing newline")
	}
	var back map[string]string
	if err := json.Unmarshal(data, &back); err != nil || back["script"] != "if a < b && c > d" {
		t.Errorf("round trip failed: %v %v", back, err)
	}
}

func TestOutputDirMirrorsProjectAndFolder(t *testing.T) {
	got := outputDir("/out", "Example.Project", `\Ara Yaygınlaştırma\Sub:Folder`)
	want := filepath.Join("/out", "Example.Project", "Ara Yaygınlaştırma", "Sub_Folder")
	if got != want {
		t.Errorf("outputDir=%q want %q", got, want)
	}
	if got := outputDir("/out", "P", ""); got != filepath.Join("/out", "P") {
		t.Errorf("project-only outputDir=%q", got)
	}
}

func TestExpandDirMakesAbsolute(t *testing.T) {
	got, err := expandDir("./rel")
	if err != nil || !filepath.IsAbs(got) {
		t.Errorf("expandDir(./rel)=%q, %v", got, err)
	}
}
