package promotereleases

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func rel(id int, sourceStatus, targetStatus string) azuredevops.Release {
	return azuredevops.Release{ID: id, Environments: []azuredevops.ReleaseEnvironmentStatus{
		{ID: id*10 + 1, Name: "QA", Status: sourceStatus},
		{ID: id*10 + 2, Name: "Prod", Status: targetStatus},
	}}
}

func TestSelectPromotionsNewestOnly(t *testing.T) {
	releases := []azuredevops.Release{rel(2, "failed", "notStarted"), rel(1, "succeeded", "notStarted")}
	if got := selectPromotions("P", releases, "QA", "Prod", false); len(got) != 0 {
		t.Fatalf("must not fall back to an older release: %+v", got)
	}
	got := selectPromotions("P", releases, "QA", "Prod", true)
	if len(got) != 1 || got[0].release.ID != 1 {
		t.Fatalf("all releases: %+v", got)
	}
}

func TestQualifiesRequiresSucceededSourceAndUnstartedTarget(t *testing.T) {
	if _, ok := qualifies(rel(1, "succeeded", "notStarted"), "qa", "prod"); !ok {
		t.Fatal("expected release to qualify")
	}
	for _, release := range []azuredevops.Release{rel(1, "rejected", "notStarted"), rel(1, "succeeded", "succeeded"), rel(1, "succeeded", "inProgress")} {
		if _, ok := qualifies(release, "QA", "Prod"); ok {
			t.Fatalf("unexpected qualification: %+v", release)
		}
	}
}
