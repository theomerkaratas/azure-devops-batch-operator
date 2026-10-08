package azuredevops

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TaskInput is one input declared by a task definition.
type TaskInput struct {
	Name         string `json:"name"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"defaultValue"`
}

// TaskDefinition is one version of a task available in the organization.
type TaskDefinition struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	FriendlyName string `json:"friendlyName"`
	Version      struct {
		Major  int  `json:"major"`
		Minor  int  `json:"minor"`
		Patch  int  `json:"patch"`
		IsTest bool `json:"isTest"`
	} `json:"version"`
	Deprecated bool        `json:"deprecated"`
	Disabled   bool        `json:"disabled"`
	Inputs     []TaskInput `json:"inputs"`
}

// TaskCatalog indexes the task definitions of an organization by task ID.
type TaskCatalog struct{ byID map[string][]TaskDefinition }

// NewTaskCatalog builds a catalog; versions of each task are sorted ascending.
func NewTaskCatalog(defs []TaskDefinition) TaskCatalog {
	c := TaskCatalog{byID: map[string][]TaskDefinition{}}
	for _, d := range defs {
		id := strings.ToLower(d.ID)
		c.byID[id] = append(c.byID[id], d)
	}
	for _, list := range c.byID {
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i].Version, list[j].Version
			if a.Major != b.Major {
				return a.Major < b.Major
			}
			if a.Minor != b.Minor {
				return a.Minor < b.Minor
			}
			return a.Patch < b.Patch
		})
	}
	return c
}

// ListTaskDefinitions reads every task definition (all versions) in the organization.
func (c Config) ListTaskDefinitions() (TaskCatalog, error) {
	u := fmt.Sprintf("%s/_apis/distributedtask/tasks?api-version=%s", c.organizationBaseURL(), c.apiVersion())
	var resp struct {
		Value []TaskDefinition `json:"value"`
	}
	if err := c.Get(u, &resp); err != nil {
		return TaskCatalog{}, err
	}
	return NewTaskCatalog(resp.Value), nil
}

// Exists reports whether any version of the task exists.
func (c TaskCatalog) Exists(id string) bool { return len(c.byID[strings.ToLower(id)]) > 0 }

// Majors returns the available major versions of a task in ascending order.
func (c TaskCatalog) Majors(id string) []int {
	var out []int
	for _, d := range c.byID[strings.ToLower(id)] {
		if len(out) == 0 || out[len(out)-1] != d.Version.Major {
			out = append(out, d.Version.Major)
		}
	}
	return out
}

// Latest returns the newest non-preview definition of the given major version.
func (c TaskCatalog) Latest(id string, major int) (TaskDefinition, bool) {
	list := c.byID[strings.ToLower(id)]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Version.Major == major && !list[i].Version.IsTest {
			return list[i], true
		}
	}
	return TaskDefinition{}, false
}

// Resolve maps a task GUID or a task name/friendly name (case-insensitive) to task IDs.
func (c TaskCatalog) Resolve(nameOrID string) []string {
	key := strings.ToLower(strings.TrimSpace(nameOrID))
	if _, ok := c.byID[key]; ok {
		return []string{key}
	}
	var ids []string
	for id, list := range c.byID {
		last := list[len(list)-1]
		if strings.ToLower(last.Name) == key || strings.ToLower(last.FriendlyName) == key {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// DisplayName returns the friendly name of a task, or its ID if unknown.
func (c TaskCatalog) DisplayName(id string) string {
	if list := c.byID[strings.ToLower(id)]; len(list) > 0 {
		return firstNonEmpty(list[len(list)-1].FriendlyName, list[len(list)-1].Name)
	}
	return id
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ParseTaskMajor extracts the major version from a task version spec such as "2.*" or "2.1.0".
func ParseTaskMajor(spec string) (int, bool) {
	head := strings.SplitN(strings.TrimSpace(spec), ".", 2)[0]
	n, err := strconv.Atoi(head)
	return n, err == nil
}
