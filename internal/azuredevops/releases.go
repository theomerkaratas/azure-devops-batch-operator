package azuredevops

import (
	"fmt"
	"sort"
	"strings"
)

// ReleaseDefinition mirrors the fields of an Azure DevOps release definition that we care about.
type ReleaseDefinition struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type releaseDefinitionsResponse struct {
	Value []ReleaseDefinition `json:"value"`
}

// ListReleaseDefinitions fetches all release definitions for a project.
func (c Config) ListReleaseDefinitions(project string) ([]ReleaseDefinition, error) {
	url := fmt.Sprintf("%s/%s/%s/_apis/release/definitions?api-version=6.0", c.OrgURL, Collection, project)
	var resp releaseDefinitionsResponse
	if err := c.Get(url, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// ParseTarget splits a "Project\Folder\SubFolder" (or "Project/Folder/SubFolder") style target
// into the project name and a lowercased subpath filter (empty if none was given).
func ParseTarget(target string) (project string, subpathFilter string, err error) {
	normalized := strings.Trim(strings.ReplaceAll(target, "/", `\`), `\`)
	parts := splitNonEmpty(normalized, `\`)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("invalid target path: %q", target)
	}

	project = parts[0]
	if len(parts) > 1 {
		subpathFilter = strings.ToLower(`\` + strings.Join(parts[1:], `\`))
	}
	return project, subpathFilter, nil
}

func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// GroupByFolder groups release definitions by their folder path, filtering to those
// matching subpathFilter (if non-empty).
func GroupByFolder(definitions []ReleaseDefinition, subpathFilter string) map[string][]string {
	folders := map[string][]string{}
	for _, d := range definitions {
		folderPath := d.Path
		if folderPath == "" {
			folderPath = `\`
		}
		if subpathFilter != "" {
			normFolder := strings.ToLower(folderPath)
			if normFolder != subpathFilter && !strings.HasPrefix(normFolder, subpathFilter+`\`) {
				continue
			}
		}
		folders[folderPath] = append(folders[folderPath], d.Name)
	}
	return folders
}

// SortedFolderNames returns the folder keys of a GroupByFolder result, sorted alphabetically.
func SortedFolderNames(folders map[string][]string) []string {
	keys := make([]string, 0, len(folders))
	for k := range folders {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
