package validatereleasebatch

import (
	"strings"
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func hasFinding(findings []issue, severity, part string) bool {
	for _, finding := range findings {
		if finding.severity == severity && strings.Contains(finding.text, part) {
			return true
		}
	}
	return false
}

func TestInspectDefinition(t *testing.T) {
	raw := map[string]interface{}{
		"environments": []interface{}{map[string]interface{}{
			"name": "Dev", "deployPhases": []interface{}{map[string]interface{}{
				"name": "Agent job", "deploymentInput": map[string]interface{}{"queueId": float64(0)},
			}},
		}},
		"artifacts": []interface{}{map[string]interface{}{
			"type": "Build", "alias": "drop", "definitionReference": map[string]interface{}{},
		}},
	}
	got := inspectDefinition(raw, []string{"Production"})
	for _, check := range []struct{ severity, part string }{
		{"ERROR", "no agent queue"}, {"ERROR", "Production"}, {"ERROR", "no build definition"},
	} {
		if !hasFinding(got, check.severity, check.part) {
			t.Errorf("missing %s %q in %+v", check.severity, check.part, got)
		}
	}
}

func TestActiveIssues(t *testing.T) {
	releases := []azuredevops.Release{{Name: "R1", Environments: []azuredevops.ReleaseEnvironmentStatus{
		{Name: "Dev", Status: "succeeded"}, {Name: "Prod", Status: "inProgress"},
	}}}
	got := activeIssues(releases)
	if len(got) != 1 || !strings.Contains(got[0].text, "Prod") {
		t.Fatalf("unexpected findings: %+v", got)
	}
}
