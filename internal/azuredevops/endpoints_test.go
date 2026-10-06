package azuredevops

import (
	"net/http"
	"testing"
)

func TestCloudOperationsUseServiceSpecificHosts(t *testing.T) {
	cfg := Config{
		Deployment: DeploymentCloud,
		OrgURL:     "https://dev.azure.com/example",
		ReleaseURL: "https://vsrm.dev.azure.com/example",
	}
	var requests []string
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		return response(http.StatusOK, `{"value":[]}`), nil
	})

	if _, err := cfg.ListProjects(); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ListReleaseDefinitions("My Project"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"https://dev.azure.com/example/_apis/projects?$top=500&api-version=7.1",
		"https://vsrm.dev.azure.com/example/My%20Project/_apis/release/definitions?api-version=7.1",
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
	for i := range want {
		if requests[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, requests[i], want[i])
		}
	}
}

func TestOnPremOperationsIncludeCollection(t *testing.T) {
	cfg := Config{
		Deployment: DeploymentOnPrem,
		OrgURL:     "https://devops.example.com/tfs",
		ReleaseURL: "https://devops.example.com/tfs",
		Collection: "Team Collection",
	}
	var requestURL string
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		requestURL = r.URL.String()
		return response(http.StatusOK, `{"value":[]}`), nil
	})

	if _, err := cfg.ListReleaseDefinitions("My Project"); err != nil {
		t.Fatal(err)
	}
	want := "https://devops.example.com/tfs/Team%20Collection/My%20Project/_apis/release/definitions?api-version=6.0"
	if requestURL != want {
		t.Fatalf("request URL = %q, want %q", requestURL, want)
	}
}

func TestDeleteDefinitionUsesReleaseEndpoint(t *testing.T) {
	cfg := Config{
		Deployment: DeploymentCloud,
		OrgURL:     "https://dev.azure.com/example",
		ReleaseURL: "https://vsrm.dev.azure.com/example",
	}
	var requestURL string
	stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		requestURL = r.URL.String()
		return response(http.StatusNoContent, ""), nil
	})

	if err := cfg.DeleteDefinition("My Project", 42, "cleanup old pipeline", true); err != nil {
		t.Fatal(err)
	}
	want := "https://vsrm.dev.azure.com/example/My%20Project/_apis/release/definitions/42?api-version=7.1&comment=cleanup+old+pipeline&forceDelete=true"
	if requestURL != want {
		t.Fatalf("request URL = %q, want %q", requestURL, want)
	}
}
