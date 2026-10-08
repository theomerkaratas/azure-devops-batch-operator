package inventory

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

const sample = `{
 "id": 7, "revision": 3, "name": "App", "path": "\\TEST",
 "variables": {"A": {"value": "1", "isSecret": false, "allowOverride": true}, "S": {"isSecret": true}},
 "variableGroups": [9, 4],
 "artifacts": [{"alias": "_build", "type": "Build", "isPrimary": true, "definitionReference": {"definition": {"name": "App-CI"}}}],
 "triggers": [{"triggerType": "schedule", "schedules": [{"startHours": 2, "startMinutes": 30, "timeZoneId": "UTC", "daysToRelease": 31}]}],
 "environments": [
  {"name": "Prod", "rank": 2, "retentionPolicy": {"daysToKeep": 30, "releasesToKeep": 3, "retainBuild": true},
   "preDeployApprovals": {"approvals": [{"isAutomated": false, "approver": {"displayName": "Ann"}}]},
   "postDeployApprovals": {"approvals": [{"isAutomated": true}]},
   "deployPhases": [{"name": "Run", "phaseType": 1, "deploymentInput": {"queueId": 0, "demands": ["b", "a"], "timeoutInMinutes": 60},
     "workflowTasks": [{"name": "Script", "version": "2.*", "enabled": true}]}]},
  {"name": "Dev", "rank": 1}
 ]}`

func record(t *testing.T) Record {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(sample), &raw); err != nil {
		t.Fatal(err)
	}
	return Build(azuredevops.Config{}, "P", raw, Options{})
}

func TestBuildNormalizes(t *testing.T) {
	r := record(t)
	if r.Label() != `P\TEST\App` || len(r.Stages) != 2 || r.Stages[0].Name != "Dev" {
		t.Fatalf("record = %+v", r)
	}
	if r.VariableGroups[0] != 4 || r.Schedules[0] != "02:30 UTC days=31" {
		t.Fatalf("groups/schedules = %v %v", r.VariableGroups, r.Schedules)
	}
	prod := r.Stages[1]
	if prod.PreApprovals != "manual: Ann" || prod.PostApprovals != "automatic" || prod.Jobs[0].Demands[0] != "a" {
		t.Fatalf("prod = %+v", prod)
	}
	if r.Variables[0].Value != "" {
		t.Fatal("values must be omitted by default")
	}
}

func TestFlattenIgnoresIdentity(t *testing.T) {
	f := record(t).Flatten()
	if f["stage Prod: retention days"] != "30" || f["variable S"] != "secret" || f["artifact _build"] != "type=Build primary=true" {
		t.Fatalf("flat = %v", f)
	}
	for k := range f {
		if strings.Contains(k, "App-CI") || strings.Contains(k, "revision") {
			t.Fatalf("identity leaked into %q", k)
		}
	}
}

func TestWriteFormats(t *testing.T) {
	recs := []Record{record(t)}
	var buf bytes.Buffer
	if err := Write(&buf, "csv", recs); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(rows) != 3 || len(rows[1]) != len(csvHeader) {
		t.Fatalf("csv rows = %d err=%v", len(rows), err)
	}
	buf.Reset()
	if err := Write(&buf, "json", recs); err != nil || !json.Valid(buf.Bytes()) {
		t.Fatalf("json invalid: %v", err)
	}
	buf.Reset()
	if err := Write(&buf, "text", recs); err != nil || !strings.Contains(buf.String(), "stage 2. Prod") {
		t.Fatalf("text = %s", buf.String())
	}
	if err := Write(&buf, "xml", recs); err == nil {
		t.Fatal("expected error for unknown format")
	}
}
