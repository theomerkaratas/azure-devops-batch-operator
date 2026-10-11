package createproject

import (
	"encoding/json"
	"fmt"
	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"io"
	"net/http"
	"reflect"
	"strings"
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
	if err := run(`["Project1", "Project 2"]`, "", "private", "Agile", "Git", "read-write", true, false); err != nil {
		t.Fatal(err)
	}
}

type rejectTransport struct{ t *testing.T }

func (r rejectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Errorf("dry-run called API: %s", req.URL)
	return nil, fmt.Errorf("unexpected API call")
}

func TestParseNames(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{"Project 2", []string{"Project 2"}},
		{`["Project1", "Project 2"]`, []string{"Project1", "Project 2"}},
		{"[\n\" Project1 \",\n\"Project 2\"\n]", []string{"Project1", "Project 2"}},
	} {
		got, err := parseNames(tc.input)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseNames(%q) = %v, %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", `[]`, `[""]`, `["one", "ONE"]`, `["one", null]`, `[1]`, `[null]`, `["one",]`, `["one"`} {
		if _, err := parseNames(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestBatchCreation(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		t.Run(fmt.Sprint(failSecond), func(t *testing.T) {
			t.Setenv("A22R_CONFIG", t.TempDir()+"/missing.yml")
			t.Setenv("A22R_DEPLOYMENT", "cloud")
			t.Setenv("AZURE_DEVOPS_URL", "https://dev.azure.com/example")
			t.Setenv("AZURE_DEVOPS_PAT_READWRITE", "test")
			var names []string
			previous := http.DefaultTransport
			http.DefaultTransport = transportFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "POST" {
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
				}
				var p azuredevops.ProjectOptions
				if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
					t.Fatal(err)
				}
				names = append(names, p.Name)
				if p.Description != "shared" || p.Capabilities["processTemplate"]["templateTypeId"] != "adcc42ab-9882-485e-a3ed-7678f01f66bc" {
					t.Errorf("unexpected options: %+v", p)
				}
				code, body := 202, `{"id":"operation","status":"succeeded"}`
				if failSecond && len(names) == 2 {
					code, body = 400, `failed`
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			err := run(`["Project1","Project 2","Project3"]`, "shared", "private", "adcc42ab-9882-485e-a3ed-7678f01f66bc", "Git", "read-write", false, true)
			want := []string{"Project1", "Project 2", "Project3"}
			if failSecond {
				want = want[:2]
				if err == nil || !strings.Contains(err.Error(), "1 project(s) completed") {
					t.Fatalf("expected partial failure, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(names, want) {
				t.Errorf("created %v, want %v", names, want)
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
