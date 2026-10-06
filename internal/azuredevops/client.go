package azuredevops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Get performs an authenticated GET request against the Azure DevOps API and decodes the JSON body into out.
func (c Config) Get(url string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
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
