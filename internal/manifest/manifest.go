// Package manifest persists a record of each batch operation (targets, original revisions,
// planned changes, results and failures) so it can be audited, resumed or rolled back.
package manifest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

// Entry statuses.
const (
	StatusPending    = "pending"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusRolledBack = "rolled-back"
)

const dirEnv = "A22R_MANIFEST_DIR"

// Entry is one pipeline touched by an operation.
type Entry struct {
	DefinitionID     int                    `json:"definitionId"`
	Name             string                 `json:"name"`
	Path             string                 `json:"path"`
	OriginalRevision int                    `json:"originalRevision"`
	Changes          []string               `json:"changes"`
	Original         map[string]interface{} `json:"original"`
	Planned          map[string]interface{} `json:"planned"`
	Status           string                 `json:"status"`
	NewRevision      int                    `json:"newRevision,omitempty"`
	RolledBackRev    int                    `json:"rolledBackRevision,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

// Manifest is the saved record of one batch operation.
type Manifest struct {
	ID      string    `json:"id"`
	Command string    `json:"command"`
	Created time.Time `json:"created"`
	Target  string    `json:"target"`
	Filter  string    `json:"filter,omitempty"`
	Project string    `json:"project"`
	Comment string    `json:"comment,omitempty"`
	Entries []Entry   `json:"entries"`

	path string
}

// Dir returns the folder manifests are stored in: A22R_MANIFEST_DIR, else "manifests" beside
// the configuration file.
func Dir() (string, error) {
	if d := strings.TrimSpace(os.Getenv(dirEnv)); d != "" {
		return d, nil
	}
	cfg, err := azuredevops.ConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), "manifests"), nil
}

// New creates an unsaved manifest with a fresh ID.
func New(command, target, filter, project, comment string) (*Manifest, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Manifest{
		ID:      now.Format("20060102-150405") + "-" + hex.EncodeToString(b[:]),
		Command: command, Created: now, Target: target, Filter: filter, Project: project, Comment: comment,
	}, nil
}

// Revision reads the "revision" number of a raw definition.
func Revision(raw map[string]interface{}) int {
	f, _ := raw["revision"].(float64)
	return int(f)
}

// Clone deep-copies a raw definition.
func Clone(raw map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	return out, json.Unmarshal(data, &out)
}

// Save writes the manifest atomically (owner-only permissions: planned definitions can hold
// secret values).
func (m *Manifest) Save() error {
	if m.path == "" {
		dir, err := Dir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		m.path = filepath.Join(dir, m.ID+".json")
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// Path is where the manifest is stored (empty before the first Save).
func (m *Manifest) Path() string { return m.path }

// Load reads the manifest with the given ID (or a unique ID prefix).
func Load(id string) (*Manifest, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, id+"*.json"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("no manifest matching %q in %s", id, dir)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%q matches %d manifests; give a longer ID", id, len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", matches[0], err)
	}
	m.path = matches[0]
	return &m, nil
}

// List returns all saved manifests, oldest first. Unreadable files are skipped.
func List() ([]*Manifest, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []*Manifest
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(data, &m) == nil {
			m.path = f
			out = append(out, &m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}

// Counts returns how many entries have each status.
func (m *Manifest) Counts() map[string]int {
	c := map[string]int{}
	for _, e := range m.Entries {
		c[e.Status]++
	}
	return c
}
