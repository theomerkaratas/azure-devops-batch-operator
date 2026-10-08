package azuredevops

import (
	"fmt"
	"sync"
)

// VariableGroup is a shared library variable group linked by one or more release definitions.
type VariableGroup struct {
	ID        int                       `json:"id"`
	Name      string                    `json:"name"`
	Variables map[string]ConfigVariable `json:"variables"`
}

var (
	variableGroupCacheMu sync.Mutex
	variableGroupCache   = map[string]map[int]VariableGroup{}
)

// GetProjectVariableGroups returns every variable group defined in a project, keyed by ID.
// Results are cached per project for the lifetime of the process.
func (c Config) GetProjectVariableGroups(project string) (map[int]VariableGroup, error) {
	variableGroupCacheMu.Lock()
	if cached, ok := variableGroupCache[project]; ok {
		variableGroupCacheMu.Unlock()
		return cached, nil
	}
	variableGroupCacheMu.Unlock()

	url := fmt.Sprintf("%s/_apis/distributedtask/variablegroups?api-version=%s", c.projectBaseURL(project), c.apiVersion())
	groups, err := GetAll[VariableGroup](c, url)
	if err != nil {
		return nil, err
	}

	mapping := make(map[int]VariableGroup, len(groups))
	for _, group := range groups {
		mapping[group.ID] = group
	}

	variableGroupCacheMu.Lock()
	variableGroupCache[project] = mapping
	variableGroupCacheMu.Unlock()
	return mapping, nil
}

// ResolveVariableGroupName resolves a variable-group ID to its name, falling back to "Group <id>"
// if it cannot be found (e.g. it was deleted or the PAT lacks access).
func (c Config) ResolveVariableGroupName(project string, groupID int) string {
	groups, err := c.GetProjectVariableGroups(project)
	if err == nil {
		if group, ok := groups[groupID]; ok {
			return group.Name
		}
	}
	return fmt.Sprintf("Group %d", groupID)
}
