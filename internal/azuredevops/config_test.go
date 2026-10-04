package azuredevops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromFile(t *testing.T) {
	path := writeTestConfig(t)
	t.Setenv(configPathEnv, path)
	t.Setenv("AZURE_DEVOPS_URL", "")
	if err := os.Unsetenv("AZURE_DEVOPS_URL"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AZURE_DEVOPS_PAT_READ", "")
	if err := os.Unsetenv("AZURE_DEVOPS_PAT_READ"); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig("read")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OrgURL != "https://dev.azure.com/example" || cfg.PAT != "read-secret" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if got := DefaultLevel("manage"); got != "read" {
		t.Fatalf("DefaultLevel() = %q, want read", got)
	}
}

func TestEnvironmentOverridesFile(t *testing.T) {
	t.Setenv(configPathEnv, writeTestConfig(t))
	t.Setenv("AZURE_DEVOPS_URL", "https://dev.azure.com/override/")
	t.Setenv("AZURE_DEVOPS_PAT_READ", "environment-secret")
	t.Setenv("A22R_DEFAULT_TOKEN", "manage")

	cfg, err := LoadConfig("read")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OrgURL != "https://dev.azure.com/override" || cfg.PAT != "environment-secret" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if got := DefaultLevel("read"); got != "manage" {
		t.Fatalf("DefaultLevel() = %q, want manage", got)
	}
}

func TestLoadCloudConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("deployment: cloud\nazure_devops_url: https://dev.azure.com/example/\ntokens:\n  read: read-secret\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configPathEnv, path)
	unsetTestEnv(t, "AZURE_DEVOPS_URL")
	unsetTestEnv(t, "AZURE_DEVOPS_PAT_READ")
	unsetTestEnv(t, "A22R_DEPLOYMENT")
	unsetTestEnv(t, "A22R_COLLECTION")

	cfg, err := LoadConfig("read")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Deployment != DeploymentCloud {
		t.Fatalf("deployment = %q, want %q", cfg.Deployment, DeploymentCloud)
	}
	if got, want := cfg.organizationBaseURL(), "https://dev.azure.com/example"; got != want {
		t.Fatalf("organizationBaseURL() = %q, want %q", got, want)
	}
	if got, want := cfg.releaseProjectBaseURL("My Project"), "https://vsrm.dev.azure.com/example/My%20Project"; got != want {
		t.Fatalf("releaseProjectBaseURL() = %q, want %q", got, want)
	}
	if got := cfg.apiVersion(); got != "7.1" {
		t.Fatalf("apiVersion() = %q, want 7.1", got)
	}
}

func TestLoadOnPremConfigWithCustomCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("deployment: on-prem\nazure_devops_url: https://devops.example.com/tfs/\ncollection: Team Collection\ntokens:\n  read: read-secret\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configPathEnv, path)
	unsetTestEnv(t, "AZURE_DEVOPS_URL")
	unsetTestEnv(t, "AZURE_DEVOPS_PAT_READ")
	unsetTestEnv(t, "A22R_DEPLOYMENT")
	unsetTestEnv(t, "A22R_COLLECTION")

	cfg, err := LoadConfig("read")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.projectBaseURL("My Project"), "https://devops.example.com/tfs/Team%20Collection/My%20Project"; got != want {
		t.Fatalf("projectBaseURL() = %q, want %q", got, want)
	}
	if got, want := cfg.releaseProjectBaseURL("My Project"), cfg.projectBaseURL("My Project"); got != want {
		t.Fatalf("releaseProjectBaseURL() = %q, want %q", got, want)
	}
	if got := cfg.apiVersion(); got != "6.0" {
		t.Fatalf("apiVersion() = %q, want 6.0", got)
	}
}

func TestLoadCloudConfigRejectsOnPremURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("deployment: cloud\nazure_devops_url: https://devops.example.com/tfs\ntokens:\n  read: read-secret\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configPathEnv, path)
	unsetTestEnv(t, "AZURE_DEVOPS_URL")
	unsetTestEnv(t, "AZURE_DEVOPS_PAT_READ")
	unsetTestEnv(t, "A22R_DEPLOYMENT")

	if _, err := LoadConfig("read"); err == nil {
		t.Fatal("LoadConfig() accepted an on-prem URL for a cloud deployment")
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("azure_devops_url: https://dev.azure.com/example/\ndefault_token: read\ntokens:\n  read: read-secret\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func unsetTestEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}
