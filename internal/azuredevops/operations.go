package azuredevops

import (
	"fmt"
	"strings"
)

// ListProjects returns the names of all projects in the organization or collection.
func (c Config) ListProjects() ([]string, error) {
	url := fmt.Sprintf("%s/_apis/projects?$top=500&api-version=%s", c.organizationBaseURL(), c.apiVersion())
	var resp struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := c.Get(url, &resp); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.Value))
	for _, p := range resp.Value {
		names = append(names, p.Name)
	}
	return names, nil
}

// Pool is an agent pool.
type Pool struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Size     int    `json:"size"`
	IsHosted bool   `json:"isHosted"`
}

// Agent is a member of an agent pool.
type Agent struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Enabled       bool   `json:"enabled"`
	Version       string `json:"version"`
	OSDescription string `json:"osDescription"`
}

// ListPools returns all agent pools of the organization or collection.
func (c Config) ListPools() ([]Pool, error) {
	url := fmt.Sprintf("%s/_apis/distributedtask/pools?api-version=%s", c.organizationBaseURL(), c.apiVersion())
	var resp struct {
		Value []Pool `json:"value"`
	}
	if err := c.Get(url, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// ListPoolAgents returns the agents (members) of a pool.
func (c Config) ListPoolAgents(poolID int) ([]Agent, error) {
	url := fmt.Sprintf("%s/_apis/distributedtask/pools/%d/agents?api-version=%s", c.organizationBaseURL(), poolID, c.apiVersion())
	var resp struct {
		Value []Agent `json:"value"`
	}
	if err := c.Get(url, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// CreateDefinitionRaw creates a new release definition (POST) from an untyped map.
func (c Config) CreateDefinitionRaw(project string, definition map[string]interface{}, comment string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/_apis/release/definitions?api-version=%s", c.releaseProjectBaseURL(project), c.apiVersion())
	if comment != "" {
		definition["comment"] = comment
	}
	var created map[string]interface{}
	if err := c.Post(url, definition, &created); err != nil {
		return nil, err
	}

	return created, nil
}

// CreateFolder creates a release folder; an already-existing folder is not an error.
func (c Config) CreateFolder(project, path string) error {
	url := fmt.Sprintf("%s/_apis/release/folders?api-version=%s", c.releaseProjectBaseURL(project), c.apiVersion())
	err := c.Post(url, map[string]string{"path": path}, nil)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return nil
	}
	return err
}

// CancelReleaseEnvironment cancels a queued or in-progress stage deployment.
func (c Config) CancelReleaseEnvironment(project string, releaseID, environmentID int) error {
	version := "6.0-preview.6"
	if c.Deployment == DeploymentCloud {
		version = c.apiVersion()
	}
	url := fmt.Sprintf(
		"%s/_apis/release/releases/%d/environments/%d?api-version=%s",
		c.releaseProjectBaseURL(project), releaseID, environmentID, version,
	)
	return c.Patch(url, map[string]string{"status": "canceled", "comment": "Canceled by azure-devops-batch-operator"}, nil)
}

// AbandonRelease abandons a release so it can no longer be deployed.
func (c Config) AbandonRelease(project string, releaseID int) error {
	url := fmt.Sprintf("%s/_apis/release/releases/%d?api-version=%s", c.releaseProjectBaseURL(project), releaseID, c.apiVersion())
	return c.Patch(url, map[string]string{"status": "abandoned", "comment": "Abandoned by azure-devops-batch-operator"}, nil)
}
