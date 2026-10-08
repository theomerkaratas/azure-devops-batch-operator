package azuredevops

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func stubHTTPClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	original := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: fn}
	t.Cleanup(func() { http.DefaultClient = original })
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestGetDecodesResponseAndAuthenticates(t *testing.T) {
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		_, password, ok := r.BasicAuth()
		if !ok || password != "secret" {
			t.Errorf("unexpected authorization header")
		}
		return response(http.StatusOK, `{"value":"ok"}`), nil
	})

	var got struct {
		Value string `json:"value"`
	}
	if err := (Config{PAT: "secret"}).Get("https://example.test", &got); err != nil {
		t.Fatal(err)
	}
	if got.Value != "ok" {
		t.Fatalf("value = %q, want ok", got.Value)
	}
}

func TestSendAcceptsEmptySuccessResponse(t *testing.T) {
	stubHTTPClient(t, func(*http.Request) (*http.Response, error) {
		return response(http.StatusNoContent, ""), nil
	})

	var out map[string]interface{}
	if err := (Config{}).Patch("https://example.test", map[string]string{"status": "done"}, &out); err != nil {
		t.Fatalf("Patch() returned an error for a successful empty response: %v", err)
	}
}

func TestGetIncludesHTTPFailureDetails(t *testing.T) {
	stubHTTPClient(t, func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, "denied"), nil
	})

	err := (Config{}).Get("https://example.test", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("Get() error = %v, want HTTP status and response details", err)
	}
}

func TestDeleteUsesDeleteMethod(t *testing.T) {
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		return response(http.StatusNoContent, ""), nil
	})

	if err := (Config{PAT: "secret"}).Delete("https://example.test/item"); err != nil {
		t.Fatal(err)
	}
}

func TestRetryReleaseEnvironmentQueuesRedeploy(t *testing.T) {
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/releases/12/environments/34") {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["status"] != "inProgress" {
			t.Errorf("status = %q, want inProgress", body["status"])
		}
		return response(http.StatusOK, ""), nil
	})

	cfg := Config{PAT: "secret", ReleaseURL: "https://vsrm.dev.azure.com/example", Deployment: DeploymentCloud}
	if err := cfg.RetryReleaseEnvironment("Project", 12, 34); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseApprovalAPIs(t *testing.T) {
	calls := 0
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if r.Method != http.MethodGet || r.URL.Query().Get("statusFilter") != "pending" || r.URL.Query().Get("releaseIdsFilter") != "10,20" {
				t.Errorf("unexpected list request: %s %s", r.Method, r.URL.String())
			}
			return response(http.StatusOK, `{"value":[{"id":7,"status":"pending","approvalType":"preDeploy"}]}`), nil
		}
		if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/approvals/7") {
			t.Errorf("unexpected update request: %s %s", r.Method, r.URL.String())
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["status"] != "approved" || body["comments"] != "ok" {
			t.Errorf("unexpected body: %#v", body)
		}
		return response(http.StatusOK, ""), nil
	})
	cfg := Config{PAT: "secret", ReleaseURL: "https://vsrm.dev.azure.com/example", Deployment: DeploymentCloud}
	values, err := cfg.ListPendingReleaseApprovals("Project", []int{10, 20}, 100)
	if err != nil || len(values) != 1 || values[0].ID != 7 {
		t.Fatalf("ListPendingReleaseApprovals() = %+v, %v", values, err)
	}
	if err := cfg.UpdateReleaseApproval("Project", 7, "approved", "ok"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateReleaseWithArtifactsPinsVersions(t *testing.T) {
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/_apis/release/releases") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var body struct {
			DefinitionID int               `json:"definitionId"`
			Artifacts    []ReleaseArtifact `json:"artifacts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.DefinitionID != 9 || len(body.Artifacts) != 1 || body.Artifacts[0].Alias != "drop" || body.Artifacts[0].InstanceReference.ID != "42" {
			t.Errorf("unexpected body: %#v", body)
		}
		return response(http.StatusOK, `{"id":100,"name":"Release-100"}`), nil
	})
	var artifact ReleaseArtifact
	artifact.Alias = "drop"
	artifact.InstanceReference.ID = "42"
	cfg := Config{PAT: "secret", ReleaseURL: "https://vsrm.dev.azure.com/example", Deployment: DeploymentCloud}
	created, err := cfg.CreateReleaseWithArtifacts("Project", 9, "rollback", []string{"Prod"}, []ReleaseArtifact{artifact})
	if err != nil || created.ID != 100 {
		t.Fatalf("CreateReleaseWithArtifacts() = %+v, %v", created, err)
	}
}
