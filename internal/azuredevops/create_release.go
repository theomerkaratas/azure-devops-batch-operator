package azuredevops

import "fmt"

// CreateRelease creates a new release for a definition. manualEnvironments names stages to
// start manually; other stages follow their configured triggers.
func (c Config) CreateRelease(project string, definitionID int, description string, manualEnvironments []string) (Release, error) {
	url := fmt.Sprintf("%s/%s/%s/_apis/release/releases?api-version=6.0", c.OrgURL, Collection, project)
	body := map[string]interface{}{
		"definitionId": definitionID,
		"description":  description,
		"isDraft":      false,
		"reason":       "none",
	}
	if len(manualEnvironments) > 0 {
		body["manualEnvironments"] = manualEnvironments
	}
	var created Release
	if err := c.Post(url, body, &created); err != nil {
		return Release{}, err
	}
	return created, nil
}
