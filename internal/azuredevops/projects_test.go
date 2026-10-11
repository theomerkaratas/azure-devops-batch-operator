package azuredevops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateProjectAndPoll(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pat, ok := r.BasicAuth(); !ok || pat != "test" {
			t.Error("missing PAT")
		}
		switch r.URL.Path {
		case "/Collection/_apis/process/processes":
			w.Write([]byte(`{"value":[{"id":"agile-id","name":"Agile","isDefault":true}]}`))
		case "/Collection/_apis/projects":
			if r.Method != "POST" || r.URL.Query().Get("api-version") != "6.0-preview.4" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			}
			var p ProjectOptions
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Error(err)
			}
			if p.Capabilities["processTemplate"]["templateTypeId"] != "agile-id" {
				t.Errorf("unexpected payload: %+v", p)
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":"operation-id","status":"queued"}`))
		case "/Collection/_apis/operations/operation-id":
			polls++
			status := "inProgress"
			if polls == 2 {
				status = "succeeded"
			}
			json.NewEncoder(w).Encode(ProjectOperation{Status: status})
		default:
			t.Errorf("unexpected path: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := Config{Deployment: DeploymentOnPrem, OrgURL: server.URL, Collection: "Collection", PAT: "test"}
	for _, name := range []string{"", "agile"} {
		id, err := cfg.ResolveProjectProcess(name)
		if err != nil || id != "agile-id" {
			t.Fatalf("resolve: %q %v", id, err)
		}
	}
	op, err := cfg.CreateProject(ProjectOptions{Name: "Example", Visibility: "private", Capabilities: map[string]map[string]string{"processTemplate": {"templateTypeId": "agile-id"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := cfg.WaitProjectOperation(ctx, op, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if polls != 2 {
		t.Errorf("polls=%d", polls)
	}
}

func TestProjectOperationFailures(t *testing.T) {
	cfg := Config{Deployment: DeploymentCloud, OrgURL: "https://dev.azure.com/example"}
	for _, status := range []string{"failed", "cancelled"} {
		err := cfg.WaitProjectOperation(context.Background(), ProjectOperation{ID: "id", Status: status, DetailedMessage: "reason"}, time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "reason") {
			t.Errorf("status %s: %v", status, err)
		}
	}
	for _, op := range []ProjectOperation{{}, {URL: "https://untrusted.example/operation"}} {
		if _, err := cfg.projectOperationURL(op); err == nil {
			t.Error("accepted invalid operation reference")
		}
	}
}

func TestProjectOperationTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	cfg := Config{Deployment: DeploymentCloud, OrgURL: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := cfg.WaitProjectOperation(ctx, ProjectOperation{ID: "id"}, time.Millisecond); err == nil {
		t.Fatal("expected timeout")
	}
}
