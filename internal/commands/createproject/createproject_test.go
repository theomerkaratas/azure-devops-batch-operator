package createproject

import (
	"fmt"
	"net/http"
	"testing"
)

func TestProjectName(t *testing.T) {
	for _, tc := range []struct {
		flag string
		args []string
		want string
		bad  bool
	}{
		{"", []string{"Example.Project"}, "Example.Project", false},
		{" Example.Project ", nil, "Example.Project", false},
		{"", nil, "", true}, {"", []string{" "}, "", true},
		{"one", []string{"two"}, "", true}, {"", []string{"one", "two"}, "", true},
	} {
		got, err := projectName(tc.flag, tc.args)
		if (err != nil) != tc.bad || got != tc.want {
			t.Errorf("projectName(%q,%v) = %q,%v", tc.flag, tc.args, got, err)
		}
	}
}

func TestPayload(t *testing.T) {
	p, err := payload("Example", "Description", "public", "Tfvc", "process-id")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Example" || p.Description != "Description" || p.Visibility != "public" || p.Capabilities["versioncontrol"]["sourceControlType"] != "Tfvc" || p.Capabilities["processTemplate"]["templateTypeId"] != "process-id" {
		t.Fatalf("unexpected payload: %#v", p)
	}
	for _, tc := range []struct{ name, visibility, source string }{{"", "private", "Git"}, {"Example", "invalid", "Git"}, {"Example", "private", "invalid"}} {
		if _, err := payload(tc.name, "", tc.visibility, tc.source, ""); err == nil {
			t.Errorf("accepted invalid options: %+v", tc)
		}
	}
}

func TestDryRunDoesNotCallAPI(t *testing.T) {
	t.Setenv("A22R_CONFIG", t.TempDir()+"/missing.yml")
	t.Setenv("A22R_DEPLOYMENT", "cloud")
	t.Setenv("AZURE_DEVOPS_URL", "https://dev.azure.com/example")
	t.Setenv("AZURE_DEVOPS_PAT_READWRITE", "test")
	previous := http.DefaultTransport
	http.DefaultTransport = rejectTransport{t}
	t.Cleanup(func() { http.DefaultTransport = previous })
	if err := run("Example", "", "private", "Agile", "Git", "read-write", true, false); err != nil {
		t.Fatal(err)
	}
}

type rejectTransport struct{ t *testing.T }

func (r rejectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Errorf("dry-run called API: %s", req.URL)
	return nil, fmt.Errorf("unexpected API call")
}
