package azuredevops

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// IsNotFound reports whether err is an HTTP 404 returned by the API.
func IsNotFound(err error) bool { return err != nil && strings.Contains(err.Error(), "(HTTP 404)") }

// BuildDefinitionInfo describes a build pipeline and the repository it builds from.
type BuildDefinitionInfo struct {
	ID       int
	Name     string
	RepoID   string
	RepoType string
}

// GetBuildDefinitionInfo reads a build pipeline by ID. A missing pipeline yields an error for which IsNotFound is true.
func (c Config) GetBuildDefinitionInfo(project string, id int) (BuildDefinitionInfo, error) {
	u := fmt.Sprintf("%s/_apis/build/definitions/%d?api-version=%s", c.projectBaseURL(project), id, c.apiVersion())
	var resp struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Repository struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"repository"`
	}
	if err := c.Get(u, &resp); err != nil {
		return BuildDefinitionInfo{}, err
	}
	if resp.ID == 0 {
		return BuildDefinitionInfo{}, fmt.Errorf("request failed (HTTP 404): build pipeline %d does not exist", id)
	}
	return BuildDefinitionInfo{ID: resp.ID, Name: resp.Name, RepoID: resp.Repository.ID, RepoType: resp.Repository.Type}, nil
}

// GitRepositoryExists reports whether an Azure Repos repository exists.
func (c Config) GitRepositoryExists(project, repoID string) (bool, error) {
	u := fmt.Sprintf("%s/_apis/git/repositories/%s?api-version=%s", c.projectBaseURL(project), url.PathEscape(repoID), c.apiVersion())
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.Get(u, &resp); err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return resp.ID != "", nil
}

// GitBranchExists reports whether a branch (refs/heads/x or x) exists in an Azure Repos repository.
func (c Config) GitBranchExists(project, repoID, branch string) (bool, error) {
	ref := strings.TrimPrefix(branch, "refs/")
	if !strings.HasPrefix(ref, "heads/") {
		ref = "heads/" + ref
	}
	u := fmt.Sprintf("%s/_apis/git/repositories/%s/refs?filter=%s&api-version=%s", c.projectBaseURL(project), url.PathEscape(repoID), url.QueryEscape(ref), c.apiVersion())
	var resp struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := c.Get(u, &resp); err != nil {
		return false, err
	}
	for _, r := range resp.Value {
		if r.Name == "refs/"+ref {
			return true, nil
		}
	}
	return false, nil
}

var (
	endpointCacheMu sync.Mutex
	endpointCache   = map[string]map[string]string{}
)

// ServiceEndpoints returns the service connections of a project as ID (lower-case) -> name, cached per process.
func (c Config) ServiceEndpoints(project string) (map[string]string, error) {
	endpointCacheMu.Lock()
	cached, ok := endpointCache[project]
	endpointCacheMu.Unlock()
	if ok {
		return cached, nil
	}
	u := fmt.Sprintf("%s/_apis/serviceendpoint/endpoints?api-version=%s", c.projectBaseURL(project), c.apiVersion())
	endpoints, err := GetAll[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](c, u)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(endpoints))
	for _, e := range endpoints {
		out[strings.ToLower(e.ID)] = e.Name
	}
	endpointCacheMu.Lock()
	endpointCache[project] = out
	endpointCacheMu.Unlock()
	return out, nil
}
