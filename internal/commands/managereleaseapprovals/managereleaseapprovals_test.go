package managereleaseapprovals

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func approval(id int, status, typ, stage string) azuredevops.ReleaseApproval {
	a := azuredevops.ReleaseApproval{ID: id, Status: status, ApprovalType: typ}
	a.ReleaseEnvironment.Name = stage
	a.ReleaseDefinition.Name = "Pipeline"
	a.Release.ID = id
	return a
}

func TestSelectApprovals(t *testing.T) {
	values := []azuredevops.ReleaseApproval{
		approval(1, "pending", "preDeploy", "Prod"),
		approval(2, "pending", "postDeploy", "Prod"),
		approval(3, "approved", "preDeploy", "Prod"),
		approval(4, "pending", "preDeploy", "QA"),
	}
	got := selectApprovals(values, "pre", "prod")
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("unexpected approvals: %+v", got)
	}
	got = selectApprovals(values, "both", "")
	if len(got) != 3 {
		t.Fatalf("both phases: %+v", got)
	}
}

func TestPhaseMatches(t *testing.T) {
	if !phaseMatches("preDeploy", "pre") || !phaseMatches("postDeploy", "post") || phaseMatches("preDeploy", "post") {
		t.Fatal("phase matching failed")
	}
}
