package azuredevops

import (
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
