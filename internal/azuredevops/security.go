package azuredevops

import (
	"fmt"
	"net/url"
	"strings"
)

// ReleaseManagementNamespace is the security namespace of classic release pipelines.
const ReleaseManagementNamespace = "c788c23e-1b46-4162-8f5e-d7585343b5de"

// SecurityAction is one permission bit of a security namespace.
type SecurityAction struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Bit         int    `json:"bit"`
}

// ACE is an access control entry: bitmasks allowed and denied to one identity at one token.
type ACE struct {
	Descriptor string `json:"descriptor"`
	Allow      int    `json:"allow"`
	Deny       int    `json:"deny"`
}

// ACL is the access control list of one security token.
type ACL struct {
	Token              string         `json:"token"`
	InheritPermissions bool           `json:"inheritPermissions"`
	AcesDictionary     map[string]ACE `json:"acesDictionary"`
}

// Identity is a user or group resolved from a descriptor.
type Identity struct {
	Descriptor  string
	DisplayName string
	IsContainer bool
}

// GetProjectID resolves a project name to its ID.
func (c Config) GetProjectID(project string) (string, error) {
	u := fmt.Sprintf("%s/_apis/projects/%s?api-version=%s", c.organizationBaseURL(), url.PathEscape(project), c.apiVersion())
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.Get(u, &resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", fmt.Errorf("project %q not found", project)
	}
	return resp.ID, nil
}

// SecurityActions lists the permission bits of a security namespace.
func (c Config) SecurityActions(namespaceID string) ([]SecurityAction, error) {
	u := fmt.Sprintf("%s/_apis/securitynamespaces/%s?api-version=%s", c.organizationBaseURL(), namespaceID, c.apiVersion())
	var resp struct {
		Value []struct {
			Actions []SecurityAction `json:"actions"`
		} `json:"value"`
	}
	if err := c.Get(u, &resp); err != nil {
		return nil, err
	}
	if len(resp.Value) == 0 {
		return nil, fmt.Errorf("security namespace %s not found", namespaceID)
	}
	return resp.Value[0].Actions, nil
}

// ListACLs returns the access control lists under token (recursively when recurse is set).
func (c Config) ListACLs(namespaceID, token string, recurse bool) ([]ACL, error) {
	u := fmt.Sprintf("%s/_apis/accesscontrollists/%s?token=%s&recurse=%t&api-version=%s",
		c.organizationBaseURL(), namespaceID, url.QueryEscape(token), recurse, c.apiVersion())
	var resp struct {
		Value []ACL `json:"value"`
	}
	if err := c.Get(u, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

func (c Config) identityBaseURL() string {
	if c.Deployment == DeploymentCloud {
		return strings.Replace(strings.TrimRight(c.OrgURL, "/"), "://dev.azure.com/", "://vssps.dev.azure.com/", 1)
	}
	return c.organizationBaseURL()
}

// ResolveIdentities looks up display names for identity descriptors. Unresolvable descriptors are omitted.
func (c Config) ResolveIdentities(descriptors []string) (map[string]Identity, error) {
	out := map[string]Identity{}
	const batch = 20
	for i := 0; i < len(descriptors); i += batch {
		end := i + batch
		if end > len(descriptors) {
			end = len(descriptors)
		}
		u := fmt.Sprintf("%s/_apis/identities?descriptors=%s&api-version=%s",
			c.identityBaseURL(), url.QueryEscape(strings.Join(descriptors[i:end], ",")), c.apiVersion())
		var resp struct {
			Value []struct {
				Descriptor          string `json:"descriptor"`
				ProviderDisplayName string `json:"providerDisplayName"`
				CustomDisplayName   string `json:"customDisplayName"`
				IsContainer         bool   `json:"isContainer"`
			} `json:"value"`
		}
		if err := c.Get(u, &resp); err != nil {
			return out, err
		}
		for _, v := range resp.Value {
			name := firstNonEmpty(v.CustomDisplayName, v.ProviderDisplayName)
			out[strings.ToLower(v.Descriptor)] = Identity{Descriptor: v.Descriptor, DisplayName: name, IsContainer: v.IsContainer}
		}
	}
	return out, nil
}
