package createfolder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestParsePaths(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{"Folder 2", []string{`\Folder 2`}},
		{`["Folder1", "Folder 2"]`, []string{`\Folder1`, `\Folder 2`}},
		{`["Deploy\\Services", "Test"]`, []string{`\Deploy\Services`, `\Test`}},
		{" /Deploy/Services/ ", []string{`\Deploy\Services`}},
	} {
		got, err := parsePaths(tc.input)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parsePaths(%q)=%v,%v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", `[]`, `[null]`, `[1]`, `[""]`, `["one",]`, `["one"`, `["one", "ONE"]`, `["Deploy/Services", "Deploy\\Services"]`, `\`, "a/../b", "a//b"} {
		if _, err := parsePaths(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestFolderInput(t *testing.T) {
	for _, tc := range []struct {
		flag string
		args []string
		bad  bool
	}{
		{"", []string{"Folder"}, false}, {`["one","two"]`, nil, false}, {"", nil, true}, {"one", []string{"two"}, true}, {"", []string{"one", "two"}, true},
	} {
		_, err := folderInput(tc.flag, tc.args)
		if (err != nil) != tc.bad {
			t.Errorf("folderInput(%q,%v): %v", tc.flag, tc.args, err)
		}
	}
}

func TestBatchRun(t *testing.T) {
	for _, mode := range []string{"success", "failure", "dry-run", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("A22R_CONFIG", t.TempDir()+"/missing.yml")
			t.Setenv("A22R_DEPLOYMENT", "cloud")
			t.Setenv("AZURE_DEVOPS_URL", "https://dev.azure.com/example")
			t.Setenv("AZURE_DEVOPS_PAT_READWRITE", "test")
			var paths []string
			previous := http.DefaultTransport
			http.DefaultTransport = transportFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "POST" || req.URL.Host != "vsrm.dev.azure.com" || req.URL.Path != "/example/Example Project/_apis/release/folders" {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				var body map[string]string
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, body["path"])
				code, text := 200, `{}`
				// Existing folders should count as successful, so later folders still run.
				if len(paths) == 1 {
					code, text = 409, "already exists"
				}
				if mode == "failure" && len(paths) == 2 {
					code, text = 500, "server failure"
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(text)), Header: make(http.Header)}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			input := `["Folder1","Folder 2","Folder3"]`
			if mode == "invalid" {
				input = `["Folder1", ""]`
			}
			err := run("Example Project", input, "read-write", mode == "dry-run", true)
			want := []string{`\Folder1`, `\Folder 2`, `\Folder3`}
			switch mode {
			case "failure":
				want = want[:2]
				if err == nil || !strings.Contains(err.Error(), "1 folder(s) completed") {
					t.Fatalf("expected partial failure: %v", err)
				}
			case "invalid":
				want = nil
				if err == nil {
					t.Fatal("expected validation error")
				}
			case "dry-run":
				want = nil
				if err != nil {
					t.Fatal(err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(paths, want) {
				t.Errorf("requests=%v, want %v", paths, want)
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWriteLevel(t *testing.T) {
	if err := run("Project", "Folder", "read", true, true); err == nil {
		t.Fatal(fmt.Sprint("read level accepted"))
	}
}
