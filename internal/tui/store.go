package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// savedInputs remembers the last form values per command id (field key -> value).
type savedInputs map[string]map[string]string

func inputsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "azure-devops-batch-operator", "inputs.json"), nil
}

func loadInputs() savedInputs {
	s := savedInputs{}
	path, err := inputsPath()
	if err != nil {
		return s
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s == nil {
		s = savedInputs{}
	}
	return s
}

// apply pre-fills fields from the last run. dry_run is never restored so a preview stays the default.
func (s savedInputs) apply(id string, fields []*field) {
	saved := s[id]
	for _, f := range fields {
		v, ok := saved[f.key]
		if !ok || f.key == "dry_run" {
			continue
		}
		if f.kind == fieldText {
			f.input.SetValue(v)
			continue
		}
		for i, c := range f.choices {
			if c == v {
				f.choiceIdx = i
			}
		}
	}
}

// remember stores the submitted values and persists them (best effort).
func (s savedInputs) remember(id string, values map[string]string) {
	kept := make(map[string]string, len(values))
	for k, v := range values {
		if k != "dry_run" {
			kept[k] = v
		}
	}
	// A secret variable's value must not be written to disk.
	if values["secret"] == "yes" {
		delete(kept, "set")
	}
	s[id] = kept

	path, err := inputsPath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}
