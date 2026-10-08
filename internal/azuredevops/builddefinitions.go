package azuredevops

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildDefinitionRef identifies a build pipeline and the project it lives in.
type BuildDefinitionRef struct {
	ID          int
	Name        string
	ProjectID   string
	ProjectName string
}

// FindBuildDefinition looks up a build pipeline by exact name (case-insensitive) in a project.
func (c Config) FindBuildDefinition(project, name string) (BuildDefinitionRef, error) {
	u := fmt.Sprintf("%s/_apis/build/definitions?name=%s&api-version=%s", c.projectBaseURL(project), url.QueryEscape(name), c.apiVersion())
	defs, err := GetAll[struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
	}](c, u)
	if err != nil {
		return BuildDefinitionRef{}, err
	}
	var found []BuildDefinitionRef
	for _, d := range defs {
		if strings.EqualFold(d.Name, name) {
			found = append(found, BuildDefinitionRef{ID: d.ID, Name: d.Name, ProjectID: d.Project.ID, ProjectName: d.Project.Name})
		}
	}
	switch len(found) {
	case 0:
		return BuildDefinitionRef{}, fmt.Errorf("build pipeline %q not found in project %s", name, project)
	case 1:
		return found[0], nil
	}
	return BuildDefinitionRef{}, fmt.Errorf("build pipeline name %q is ambiguous in project %s (%d matches)", name, project, len(found))
}
