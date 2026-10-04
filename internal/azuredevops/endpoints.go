package azuredevops

import (
	"net/url"
	"strings"
)

func (c Config) apiVersion() string {
	if c.Deployment == DeploymentCloud {
		return "7.1"
	}
	return "6.0"
}

func (c Config) organizationBaseURL() string {
	if c.Deployment == DeploymentCloud {
		return c.OrgURL
	}
	collection := c.Collection
	if collection == "" {
		collection = defaultCollection
	}
	return strings.TrimRight(c.OrgURL, "/") + "/" + url.PathEscape(collection)
}

func (c Config) projectBaseURL(project string) string {
	return c.organizationBaseURL() + "/" + url.PathEscape(project)
}

func (c Config) releaseProjectBaseURL(project string) string {
	if c.Deployment == DeploymentCloud {
		return strings.TrimRight(c.ReleaseURL, "/") + "/" + url.PathEscape(project)
	}
	return c.projectBaseURL(project)
}
