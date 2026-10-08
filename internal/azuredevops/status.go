package azuredevops

import (
	"fmt"
	"strings"
)

// ReleaseEnvironmentStatus is the status of a single stage within a release.
type ReleaseEnvironmentStatus struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Release is a release run, with its per-stage statuses.
type Release struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	CreatedOn   string `json:"createdOn"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	KeepForever bool   `json:"keepForever"`
	Description string `json:"description"`
	CreatedBy   struct {
		DisplayName string `json:"displayName"`
	} `json:"createdBy"`
	Environments []ReleaseEnvironmentStatus `json:"environments"`
}

// ListReleases returns up to top of the most recent releases (newest first) for a definition,
// including per-stage statuses.
func (c Config) ListReleases(project string, definitionID, top int) ([]Release, error) {
	url := fmt.Sprintf(
		"%s/_apis/release/releases?definitionId=%d&$top=%d&$expand=environments&queryOrder=descending&api-version=%s",
		c.releaseProjectBaseURL(project), definitionID, top, c.apiVersion(),
	)
	var list releasesListResponse
	if err := c.Get(url, &list); err != nil {
		return nil, err
	}

	return list.Value, nil
}

type releasesListResponse struct {
	Value []Release `json:"value"`
}

// GetLatestRelease returns the most recently created release (with environment statuses) for a
// definition, or nil if no release has ever been created.
func (c Config) GetLatestRelease(project string, definitionID int) (*Release, error) {
	listURL := fmt.Sprintf(
		"%s/_apis/release/releases?definitionId=%d&$top=1&queryOrder=descending&api-version=%s",
		c.releaseProjectBaseURL(project), definitionID, c.apiVersion(),
	)
	var list releasesListResponse
	if err := c.Get(listURL, &list); err != nil {
		return nil, err
	}
	if len(list.Value) == 0 {
		return nil, nil
	}

	detailURL := fmt.Sprintf("%s/_apis/release/releases/%d?api-version=%s", c.releaseProjectBaseURL(project), list.Value[0].ID, c.apiVersion())
	var detail Release
	if err := c.Get(detailURL, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// FindDefinitionsUnderPath returns the project and the release definitions matching a target
// project/folder path, or a single release definition if target points at an exact pipeline.
func (c Config) FindDefinitionsUnderPath(target string) (project string, matched []ReleaseDefinition, err error) {
	project, subpath, err := ParseTarget(target)
	if err != nil {
		return "", nil, err
	}

	definitions, err := c.ListReleaseDefinitions(project)
	if err != nil {
		return "", nil, err
	}

	if subpath == "" {
		return project, definitions, nil
	}

	for _, d := range definitions {
		folder := d.Path
		if folder == "" {
			folder = `\`
		}
		folder = strings.ToLower(folder)
		fullDefPath := folder + `\` + strings.ToLower(d.Name)

		switch {
		case fullDefPath == subpath:
			// An exact pipeline path was given.
			return project, []ReleaseDefinition{d}, nil
		case folder == subpath || strings.HasPrefix(folder, subpath+`\`):
			matched = append(matched, d)
		}
	}
	return project, matched, nil
}

// ListReleasesPage returns one page of releases (newest first) with environment statuses. Pass the
// ID of the last release of the previous page as continuation (0 for the first page).
func (c Config) ListReleasesPage(project string, definitionID, top, continuation int) ([]Release, error) {
	url := fmt.Sprintf(
		"%s/_apis/release/releases?definitionId=%d&$top=%d&$expand=environments&queryOrder=descending&api-version=%s",
		c.releaseProjectBaseURL(project), definitionID, top, c.apiVersion(),
	)
	if continuation > 0 {
		url += fmt.Sprintf("&continuationToken=%d", continuation)
	}
	var list releasesListResponse
	if err := c.Get(url, &list); err != nil {
		return nil, err
	}
	return list.Value, nil
}

// DeleteRelease permanently deletes a release.
func (c Config) DeleteRelease(project string, releaseID int) error {
	url := fmt.Sprintf("%s/_apis/release/releases/%d?api-version=%s", c.releaseProjectBaseURL(project), releaseID, c.apiVersion())
	return c.Delete(url)
}

// GetRelease reads one release with its per-stage statuses.
func (c Config) GetRelease(project string, releaseID int) (Release, error) {
	url := fmt.Sprintf("%s/_apis/release/releases/%d?api-version=%s", c.releaseProjectBaseURL(project), releaseID, c.apiVersion())
	var rel Release
	err := c.Get(url, &rel)
	return rel, err
}
