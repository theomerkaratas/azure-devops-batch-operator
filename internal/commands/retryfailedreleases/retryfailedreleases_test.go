package retryfailedreleases

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func release(id int, statuses ...string) azuredevops.Release {
	r := azuredevops.Release{ID: id, Name: "R"}
	for i, status := range statuses {
		r.Environments = append(r.Environments, azuredevops.ReleaseEnvironmentStatus{ID: i + 1, Name: []string{"Dev", "Prod"}[i%2], Status: status})
	}
	return r
}

func TestSelectJobsNewestOnly(t *testing.T) {
	releases := []azuredevops.Release{release(3, "notStarted"), release(2, "rejected", "succeeded"), release(1, "rejected")}
	got := selectJobs("P", releases, "", false, false)
	if len(got) != 1 || got[0].release.ID != 2 || len(got[0].envs) != 1 {
		t.Fatalf("unexpected jobs: %+v", got)
	}
}

func TestSelectJobsFiltersAndCanceled(t *testing.T) {
	releases := []azuredevops.Release{release(2, "canceled", "partiallySucceeded"), release(1, "rejected")}
	got := selectJobs("P", releases, "Prod", true, false)
	if len(got) != 1 || got[0].release.ID != 2 || got[0].envs[0].Name != "Prod" {
		t.Fatalf("stage filter: %+v", got)
	}
	got = selectJobs("P", releases, "Dev", true, true)
	if len(got) != 1 || got[0].release.ID != 2 {
		t.Fatalf("include canceled/all failed: %+v", got)
	}
}

func TestSelectJobsSkipsFailureSupersededByNewerDeployment(t *testing.T) {
	for _, newerStatus := range []string{"succeeded", "rejected", "inProgress", "queued", "canceled"} {
		releases := []azuredevops.Release{release(2, newerStatus), release(1, "rejected")}
		got := selectJobs("P", releases, "Dev", true, false)
		if newerStatus == "rejected" {
			if len(got) != 1 || got[0].release.ID != 2 {
				t.Errorf("newer %s: unexpected jobs %+v", newerStatus, got)
			}
		} else if len(got) != 0 {
			t.Errorf("newer %s should supersede old failure: %+v", newerStatus, got)
		}
	}
}

func TestAllFailedStillSelectsUnrelatedStages(t *testing.T) {
	releases := []azuredevops.Release{release(2, "rejected"), release(1, "notStarted", "rejected")}
	got := selectJobs("P", releases, "", true, false)
	if len(got) != 2 || got[0].envs[0].Name != "Dev" || got[1].envs[0].Name != "Prod" {
		t.Fatalf("unexpected jobs: %+v", got)
	}
}
