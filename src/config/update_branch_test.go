package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeUpdateBranch(t *testing.T) {
	for input, want := range map[string]string{
		"stable":      "stable",
		"BETA":        "beta",
		"devel":       "devel",
		"daily":       "devel",
		"rolling":     "devel",
		"dev":         "devel",
		"development": "devel",
		"  DAILY  ":   "devel",
	} {
		got, ok := NormalizeUpdateBranch(input)
		if !ok || got != want {
			t.Errorf("NormalizeUpdateBranch(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}

	if got, ok := NormalizeUpdateBranch("unknown"); ok {
		t.Errorf("NormalizeUpdateBranch(unknown) = %q, true; want invalid", got)
	}
}

func TestLoadNormalizesUpdateBranchAliases(t *testing.T) {
	mu.Lock()
	previousConfig := current
	previousPath := configPath
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = previousConfig
		configPath = previousPath
		mu.Unlock()
	})

	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  update:\n    branch: daily\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Update.Branch != "devel" {
		t.Fatalf("loaded update branch = %q, want devel", cfg.Server.Update.Branch)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "yaml.d", "maintenance.yml"))
	if err != nil {
		t.Fatalf("maintenance area was not created: %v", err)
	}
	if !strings.Contains(string(data), "branch: devel") {
		t.Fatalf("normalized branch was not written to maintenance area: %s", data)
	}
}

func TestLoadUsesDefaultForUnknownUpdateBranch(t *testing.T) {
	mu.Lock()
	previousConfig := current
	previousPath := configPath
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = previousConfig
		configPath = previousPath
		mu.Unlock()
	})

	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  update:\n    branch: unknown\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load rejected an unknown update branch: %v", err)
	}
	if want := DefaultConfig().Server.Update.Branch; cfg.Server.Update.Branch != want {
		t.Fatalf("loaded update branch = %q, want default %q", cfg.Server.Update.Branch, want)
	}
}
