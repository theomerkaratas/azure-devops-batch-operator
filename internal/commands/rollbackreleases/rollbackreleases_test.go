package rollbackreleases

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

func release(id int, statuses ...string) azuredevops.Release {
	r := azuredevops.Release{ID: id}
	for _, status := range statuses {
		r.Environments = append(r.Environments, azuredevops.ReleaseEnvironmentStatus{Status: status})
	}
	return r
}

func TestPreviousSuccessfulAlwaysSkipsCurrent(t *testing.T) {
	releases := []azuredevops.Release{
		release(3, "succeeded"), release(2, "rejected"), release(1, "succeeded", "notStarted"),
	}
	got, ok := previousSuccessful(releases)
	if !ok || got.ID != 1 {
		t.Fatalf("got %+v, %v", got, ok)
	}
}

func TestFullySuccessfulRejectsActiveAndFailedStages(t *testing.T) {
	if !fullySuccessful(release(1, "succeeded", "notStarted")) {
		t.Fatal("succeeded release with an unstarted manual stage should qualify")
	}
	for _, status := range []string{"rejected", "partiallySucceeded", "canceled", "inProgress", "queued"} {
		if fullySuccessful(release(1, "succeeded", status)) {
			t.Errorf("status %s should not qualify", status)
		}
	}
	abandoned := release(1, "succeeded")
	abandoned.Status = "abandoned"
	if fullySuccessful(abandoned) {
		t.Fatal("abandoned release should not qualify")
	}
}

func TestValidArtifacts(t *testing.T) {
	var artifact azuredevops.ReleaseArtifact
	artifact.Alias = "drop"
	artifact.InstanceReference.ID = "42"
	if !validArtifacts([]azuredevops.ReleaseArtifact{artifact}) || validArtifacts(nil) {
		t.Fatal("artifact validation failed")
	}
}
