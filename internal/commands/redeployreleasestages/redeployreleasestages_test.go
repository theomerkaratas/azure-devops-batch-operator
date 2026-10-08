package redeployreleasestages

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func rel(id int, stage, status string) azuredevops.Release {
	return azuredevops.Release{ID: id, Name: "R", Environments: []azuredevops.ReleaseEnvironmentStatus{{ID: id * 10, Name: stage, Status: status}}}
}

func TestSelectJobs(t *testing.T) {
	releases := []azuredevops.Release{rel(3, "Prod", "inProgress"), rel(2, "Prod", "succeeded"), rel(1, "Prod", "rejected")}
	got := selectJobs("P", releases, "prod", false)
	if len(got) != 1 || got[0].release.ID != 2 {
		t.Fatalf("newest eligible: %+v", got)
	}
	got = selectJobs("P", releases, "Prod", true)
	if len(got) != 2 || got[1].release.ID != 1 {
		t.Fatalf("all eligible: %+v", got)
	}
}

func TestEligibleExcludesUnstartedAndActive(t *testing.T) {
	for _, status := range []string{"notStarted", "inProgress", "queued", "scheduled"} {
		if eligible(status) {
			t.Errorf("%s should not be eligible", status)
		}
	}
}
