package azuredevops

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ReleaseApproval is a pending pre- or post-deployment approval on a classic release.
type ReleaseApproval struct {
	ID           int    `json:"id"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	ApprovalType string `json:"approvalType"`
	Comments     string `json:"comments"`
	Approver     struct {
		DisplayName string `json:"displayName"`
		UniqueName  string `json:"uniqueName"`
	} `json:"approver"`
	Release struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"release"`
	ReleaseDefinition struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"releaseDefinition"`
	ReleaseEnvironment struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"releaseEnvironment"`
}

// ListPendingReleaseApprovals returns pending approvals for the supplied release IDs.
func (c Config) ListPendingReleaseApprovals(project string, releaseIDs []int, top int) ([]ReleaseApproval, error) {
	if len(releaseIDs) == 0 {
		return nil, nil
	}
	ids := make([]string, len(releaseIDs))
	for i, id := range releaseIDs {
		ids[i] = strconv.Itoa(id)
	}
	query := url.Values{
		"statusFilter":            {"pending"},
		"releaseIdsFilter":        {strings.Join(ids, ",")},
		"includeMyGroupApprovals": {"true"},
		"queryOrder":              {"ascending"},
		"top":                     {strconv.Itoa(top)},
		"api-version":             {c.apiVersion()},
	}
	base := fmt.Sprintf("%s/_apis/release/approvals", c.releaseProjectBaseURL(project))
	var all []ReleaseApproval
	for page := 0; page < maxPages && len(all) < top; page++ {
		var response struct {
			Value []ReleaseApproval `json:"value"`
		}
		header, err := c.get(base+"?"+query.Encode(), &response)
		if err != nil {
			return nil, err
		}
		all = append(all, response.Value...)
		token := header.Get("x-ms-continuationtoken")
		if token == "" || len(response.Value) == 0 {
			break
		}
		query.Set("continuationToken", token)
	}
	if len(all) > top {
		all = all[:top]
	}
	return all, nil
}

// UpdateReleaseApproval approves or rejects one pending classic-release approval.
func (c Config) UpdateReleaseApproval(project string, approvalID int, status, comments string) error {
	endpoint := fmt.Sprintf("%s/_apis/release/approvals/%d?api-version=%s", c.releaseProjectBaseURL(project), approvalID, c.apiVersion())
	return c.Patch(endpoint, map[string]interface{}{"status": status, "comments": comments}, nil)
}
