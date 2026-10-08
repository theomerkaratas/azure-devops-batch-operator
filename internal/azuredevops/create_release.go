package azuredevops

import "fmt"

// CreateRelease creates a new release for a definition. manualEnvironments names stages to
// start manually; other stages follow their configured triggers.
func (c Config) CreateRelease(project string, definitionID int, description string, manualEnvironments []string) (Release, error) {
	return c.CreateReleaseWithArtifacts(project, definitionID, description, manualEnvironments, nil)
}

// CreateReleaseWithArtifacts creates a release pinned to the supplied artifact versions. When
// artifacts is nil, Azure DevOps resolves the definition's latest versions.
func (c Config) CreateReleaseWithArtifacts(project string, definitionID int, description string, manualEnvironments []string, artifacts []ReleaseArtifact) (Release, error) {
	url := fmt.Sprintf("%s/_apis/release/releases?api-version=%s", c.releaseProjectBaseURL(project), c.apiVersion())
	body := map[string]interface{}{
		"definitionId": definitionID,
		"description":  description,
		"isDraft":      false,
		"reason":       "none",
	}
	if len(manualEnvironments) > 0 {
		body["manualEnvironments"] = manualEnvironments
	}
	if artifacts != nil {
		body["artifacts"] = artifacts
	}
	var created Release
	if err := c.Post(url, body, &created); err != nil {
		return Release{}, err
	}
	return created, nil
}
