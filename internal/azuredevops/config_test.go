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

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("azure_devops_url: https://dev.azure.com/example/\ndefault_token: read\ntokens:\n  read: read-secret\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
