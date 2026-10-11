package azuredevops

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ProjectOptions describes a new team project.
type ProjectOptions struct {
	Name         string                       `json:"name"`
	Description  string                       `json:"description,omitempty"`
	Visibility   string                       `json:"visibility"`
	Capabilities map[string]map[string]string `json:"capabilities"`
}

// ProjectOperation is the asynchronous result of project creation.
type ProjectOperation struct {
	ID              string `json:"id"`
	URL             string `json:"url"`
	PluginID        string `json:"pluginId"`
	Status          string `json:"status"`
	DetailedMessage string `json:"detailedMessage"`
	ResultMessage   string `json:"resultMessage"`
}

var processIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ResolveProjectProcess resolves a process name, or the organization's default, to its ID.
func (c Config) ResolveProjectProcess(name string) (string, error) {
	if processIDPattern.MatchString(name) {
		return name, nil
	}
	processes, err := GetAll[struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsDefault bool   `json:"isDefault"`
	}](c, c.organizationBaseURL()+"/_apis/process/processes?api-version="+c.apiVersion())
	if err != nil {
		return "", fmt.Errorf("list process templates: %w", err)
	}
	for _, p := range processes {
		if (name == "" && p.IsDefault) || (name != "" && strings.EqualFold(p.Name, name)) {
			return p.ID, nil
		}
	}
	if name == "" {
		return "", fmt.Errorf("no default process template found; specify --process-template")
	}
	return "", fmt.Errorf("process template %q not found", name)
}

// CreateProject queues creation using the Core API at organization/collection scope.
func (c Config) CreateProject(project ProjectOptions) (ProjectOperation, error) {
	version := "6.0-preview.4"
	if c.Deployment == DeploymentCloud {
		version = "7.1-preview.4"
	}
	var op ProjectOperation
	err := c.Post(c.organizationBaseURL()+"/_apis/projects?api-version="+version, project, &op)
	return op, err
}

// WaitProjectOperation waits until creation finishes. The context bounds both requests and waits.
func (c Config) WaitProjectOperation(ctx context.Context, op ProjectOperation, interval time.Duration) error {
	endpoint, err := c.projectOperationURL(op)
	if err != nil {
		return err
	}
	for {
		switch strings.ToLower(op.Status) {
		case "succeeded":
			return nil
		case "failed", "cancelled", "canceled":
			return fmt.Errorf("project creation %s: %s %s", op.Status, op.ResultMessage, op.DetailedMessage)
		case "", "notset", "queued", "inprogress":
		default:
			return fmt.Errorf("unexpected operation status %q", op.Status)
		}
		if err := c.GetContext(ctx, endpoint, &op); err != nil {
			return fmt.Errorf("poll project creation (%s); creation may still complete: %w", endpoint, err)
		}
		switch strings.ToLower(op.Status) {
		case "", "notset", "queued", "inprogress":
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("waiting for project creation (%s); creation may still complete: %w", endpoint, ctx.Err())
			case <-timer.C:
			}
		}
	}
}

func (c Config) projectOperationURL(op ProjectOperation) (string, error) {
	base, err := url.Parse(c.organizationBaseURL() + "/")
	if err != nil {
		return "", err
	}
	var endpoint *url.URL
	if op.URL != "" {
		ref, err := url.Parse(op.URL)
		if err != nil {
			return "", err
		}
		endpoint = base.ResolveReference(ref)
		// Never send a PAT to a different host supplied in an operation response.
		if endpoint.Scheme != base.Scheme || endpoint.Host != base.Host || endpoint.User != nil {
			return "", fmt.Errorf("operation URL is outside the configured Azure DevOps server")
		}
	} else {
		if op.ID == "" {
			return "", fmt.Errorf("project creation returned no operation ID or URL")
		}
		endpoint, err = url.Parse(c.organizationBaseURL() + "/_apis/operations/" + url.PathEscape(op.ID))
		if err != nil {
			return "", err
		}
	}
	query := endpoint.Query()
	query.Set("api-version", c.apiVersion())
	if op.PluginID != "" {
		query.Set("pluginId", op.PluginID)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}
