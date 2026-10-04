// Package azuredevops provides shared helpers for talking to the Azure DevOps REST API.
package azuredevops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Collection is the Azure DevOps collection shared by all scripts.
const Collection = "DefaultCollection"

const configPathEnv = "A22R_CONFIG"

// patEnvVars maps a PAT authorization level to its environment variable name.
var patEnvVars = map[string]string{
	"read":       "AZURE_DEVOPS_PAT_READ",
	"read-write": "AZURE_DEVOPS_PAT_READWRITE",
	"manage":     "AZURE_DEVOPS_PAT_MANAGE",
}

type fileConfig struct {
	AzureDevOpsURL string `yaml:"azure_devops_url"`
	DefaultToken   string `yaml:"default_token"`
	Tokens         struct {
		Read      string `yaml:"read"`
		ReadWrite string `yaml:"read_write"`
		Manage    string `yaml:"manage"`
	} `yaml:"tokens"`
}

// Levels returns the valid --level choices, in a stable order.
func Levels() []string {
	return []string{"read", "read-write", "manage"}
}

// ConfigPath returns the platform-specific configuration file path. A22R_CONFIG
// can point to a different file, which is also useful for portable installs.
func ConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(configPathEnv)); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "a22r", "config.yml"), nil
}

// Config holds the values needed to call the Azure DevOps API.
type Config struct {
	OrgURL string
	PAT    string
}

// DefaultLevel returns the configured default token level, or fallback when no
// default is configured. A22R_DEFAULT_TOKEN overrides the configuration file.
func DefaultLevel(fallback string) string {
	if level := strings.TrimSpace(os.Getenv("A22R_DEFAULT_TOKEN")); validLevel(level) {
		return level
	}
	fc, err := readFileConfig()
	if err == nil && validLevel(fc.DefaultToken) {
		return fc.DefaultToken
	}
	return fallback
}

// LoadConfig loads the Azure DevOps URL and token from the configuration file.
// The corresponding environment variable overrides each file value.
func LoadConfig(level string) (Config, error) {
	envVar, ok := patEnvVars[level]
	if !ok {
		return Config{}, fmt.Errorf("invalid level: %q (choices: %s)", level, strings.Join(Levels(), ", "))
	}

	fc, fileErr := readFileConfig()
	orgURL := strings.TrimSpace(fc.AzureDevOpsURL)
	pat := tokenFromFile(fc, level)
	if value, ok := os.LookupEnv("AZURE_DEVOPS_URL"); ok {
		orgURL = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv(envVar); ok {
		pat = strings.TrimSpace(value)
	}

	if orgURL == "" || pat == "" {
		path, pathErr := ConfigPath()
		if pathErr != nil {
			path = "the a22r configuration file"
		}
		if fileErr != nil && !errors.Is(fileErr, os.ErrNotExist) {
			return Config{}, fileErr
		}
		if orgURL == "" {
			return Config{}, fmt.Errorf("Azure DevOps URL is not configured; set AZURE_DEVOPS_URL or azure_devops_url in %s", path)
		}
		return Config{}, fmt.Errorf("%s token is not configured; set %s or tokens.%s in %s", level, envVar, strings.ReplaceAll(level, "-", "_"), path)
	}

	return Config{OrgURL: strings.TrimRight(orgURL, "/"), PAT: pat}, nil
}

func readFileConfig() (fileConfig, error) {
	var fc fileConfig
	path, err := ConfigPath()
	if err != nil {
		return fc, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fc, err
	}
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return fc, fmt.Errorf("parse config file %s: %w", path, err)
	}
	fc.DefaultToken = strings.TrimSpace(fc.DefaultToken)
	return fc, nil
}

func tokenFromFile(fc fileConfig, level string) string {
	switch level {
	case "read":
		return strings.TrimSpace(fc.Tokens.Read)
	case "read-write":
		return strings.TrimSpace(fc.Tokens.ReadWrite)
	case "manage":
		return strings.TrimSpace(fc.Tokens.Manage)
	default:
		return ""
	}
}

func validLevel(level string) bool {
	_, ok := patEnvVars[level]
	return ok
}
