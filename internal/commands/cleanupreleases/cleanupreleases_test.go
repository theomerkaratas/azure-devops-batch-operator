package cleanupreleases

import (
	"testing"
	"time"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

var now = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

func rel(id int, daysAgo int, status string, envs ...string) azuredevops.Release {
	r := azuredevops.Release{ID: id, Name: "R", Status: "active", CreatedOn: now.AddDate(0, 0, -daysAgo).Format(time.RFC3339)}
	if status != "" {
		r.Status = status
	}
	for _, e := range envs {
		r.Environments = append(r.Environments, azuredevops.ReleaseEnvironmentStatus{Status: e})
	}
	return r
}

func ids(c []candidate) []int {
	var out []int
	for _, x := range c {
		out = append(out, x.release.ID)
	}
	return out
}

func TestClassify(t *testing.T) {
	cases := map[string]azuredevops.Release{
		"succeeded":   rel(1, 1, "", "succeeded", "notStarted"),
		"failed":      rel(2, 1, "", "succeeded", "rejected"),
		"canceled":    rel(3, 1, "", "canceled"),
		"inprogress":  rel(4, 1, "", "succeeded", "inProgress"),
		"notdeployed": rel(5, 1, "", "notStarted"),
		"abandoned":   rel(6, 1, "abandoned"),
	}
	for want, r := range cases {
		if got := classify(r); got != want {
			t.Errorf("classify = %s, want %s", got, want)
		}
	}
}

func TestSelectProtections(t *testing.T) {
	forever := rel(10, 400, "", "succeeded")
	forever.KeepForever = true
	releases := []azuredevops.Release{
		rel(1, 1, "", "succeeded"), // newest: protected by keep-latest
		rel(2, 200, "", "succeeded"),
		rel(3, 300, "", "succeeded"),
		rel(4, 350, "", "rejected"),
		rel(5, 360, "", "inProgress"),
		forever,
		rel(7, 5, "abandoned"), // too young
	}
	got := ids(selectForDeletion(releases, rules{olderThanDays: 90, keepLatest: 1, keepSuccessful: 1}, now))
	// id 2 is the newest succeeded after keep-latest ... keep-successful=1 counts id1 as the first success, so id2 is deletable.
	want := []int{2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	byStatus := ids(selectForDeletion(releases, rules{statuses: map[string]bool{"failed": true}}, now))
	if len(byStatus) != 1 || byStatus[0] != 4 {
		t.Fatalf("status rule: %v", byStatus)
	}
}

func TestHugeOlderThanNeverSelectsRecent(t *testing.T) {
	releases := []azuredevops.Release{rel(1, 0, "abandoned"), rel(2, 1, "abandoned")}
	if got := selectForDeletion(releases, rules{olderThanDays: maxOlderThanDays}, now); len(got) != 0 {
		t.Fatalf("recent releases selected: %v", ids(got))
	}
}
