package detectbrokenartifactreferences

import (
	"errors"
	"strings"
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

type fake struct {
	builds map[int]azuredevops.BuildDefinitionInfo
	repos  map[string]bool
	refs   map[string]bool
	conns  map[string]string
}

func (f fake) buildDefinition(_ string, id int) (azuredevops.BuildDefinitionInfo, error) {
	if b, ok := f.builds[id]; ok {
		return b, nil
	}
	return azuredevops.BuildDefinitionInfo{}, errors.New("request failed (HTTP 404): gone")
}
func (f fake) repository(_, id string) (bool, error)       { return f.repos[id], nil }
func (f fake) branch(_, r, b string) (bool, error)         { return f.refs[r+"@"+b], nil }
func (f fake) endpoints(string) (map[string]string, error) { return f.conns, nil }

func TestInspect(t *testing.T) {
	const conn = "11111111-2222-3333-4444-555555555555"
	raw := obj{
		"artifacts": []interface{}{
			obj{"alias": "_ok", "type": "Build", "definitionReference": obj{"project": obj{"id": "p"}, "definition": obj{"id": "1"}, "defaultVersionBranch": obj{"id": "refs/heads/main"}}},
			obj{"alias": "_gone", "type": "Build", "definitionReference": obj{"project": obj{"id": "p"}, "definition": obj{"id": "2"}}},
			obj{"alias": "_nobranch", "type": "Build", "definitionReference": obj{"project": obj{"id": "p"}, "definition": obj{"id": "3"}, "defaultVersionBranch": obj{"id": "refs/heads/old"}}},
		},
		"environments": []interface{}{obj{"name": "Prod", "deployPhases": []interface{}{obj{"workflowTasks": []interface{}{
			obj{"name": "Deploy", "inputs": obj{"azureSubscription": conn}}}}}}},
	}
	f := fake{
		builds: map[int]azuredevops.BuildDefinitionInfo{1: {ID: 1, RepoID: "r", RepoType: "TfsGit"}, 3: {ID: 3, RepoID: "r", RepoType: "TfsGit"}},
		repos:  map[string]bool{"r": true},
		refs:   map[string]bool{"r@refs/heads/main": true},
		conns:  map[string]string{},
	}
	var broken []string
	for _, x := range inspect(raw, "p", f) {
		if x.broken {
			broken = append(broken, x.text)
		}
	}
	joined := strings.Join(broken, "\n")
	for _, want := range []string{"_gone", "refs/heads/old", conn} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding for %q in:\n%s", want, joined)
		}
	}
	if len(broken) != 3 {
		t.Fatalf("broken = %v", broken)
	}
	f.conns[conn] = "x"
	for _, x := range inspect(raw, "p", f) {
		if strings.Contains(x.text, conn) {
			t.Fatal("existing connection reported")
		}
	}
}
