package azuredevops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
)

// Get performs an authenticated GET request against the Azure DevOps API and decodes the JSON body into out.
func (c Config) Get(url string, out interface{}) error {
	_, err := c.get(url, out)
	return err
}

// get is Get that also returns the response headers.
func (c Config) get(url string, out interface{}) (http.Header, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("", c.PAT)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	if out == nil || len(body) == 0 {
		return resp.Header, nil
	}
	return resp.Header, json.Unmarshal(body, out)
}

const maxPages = 10000

// GetAll fetches a list endpoint returning {"value":[...]} and follows the x-ms-continuationtoken
// response header until the server stops sending one, so large projects are never cut off at the
// first page. rawURL must already contain a query string.
func GetAll[T any](c Config, rawURL string) ([]T, error) {
	var all []T
	next := rawURL
	seen := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		var resp struct {
			Value []T `json:"value"`
		}
		header, err := c.get(next, &resp)
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Value...)
		token := header.Get("x-ms-continuationtoken")
		if token == "" {
			return all, nil
		}
		if seen[token] {
			return nil, fmt.Errorf("pagination did not advance (repeated continuation token %q) for %s", token, rawURL)
		}
		seen[token] = true
		next = rawURL + "&continuationToken=" + neturl.QueryEscape(token)
	}
	return nil, fmt.Errorf("more than %d pages returned for %s", maxPages, rawURL)
}

// Put performs an authenticated PUT request with a JSON body against the Azure DevOps API and
// decodes the JSON response into out.
func (c Config) Put(url string, body interface{}, out interface{}) error {
	return c.send(http.MethodPut, url, body, out)
}

// Post performs an authenticated POST request with a JSON body against the Azure DevOps API and
// decodes the JSON response into out.
func (c Config) Post(url string, body interface{}, out interface{}) error {
	return c.send(http.MethodPost, url, body, out)
}

// Patch performs an authenticated PATCH request with a JSON body against the Azure DevOps API
// and decodes the JSON response into out.
func (c Config) Patch(url string, body interface{}, out interface{}) error {
	return c.send(http.MethodPatch, url, body, out)
}

// Delete performs an authenticated DELETE request against the Azure DevOps API.
func (c Config) Delete(url string) error {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("", c.PAT)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c Config) send(method, url string, body interface{}, out interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth("", c.PAT)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}
