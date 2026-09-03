// Package config loads and saves the CLI's cli.yml configuration file.
// Boolean parsing reuses github.com/apimgr/gitignore/src/config's
// ParseBool/IsTruthy/IsFalsy helpers rather than duplicating the truthy/
// falsey vocabulary — per AI.md PART 32 "CLI and Agent binaries use the
// SAME truthy/falsey parsing as the server."
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	clipath "github.com/apimgr/gitignore/src/client/path"
	srvconfig "github.com/apimgr/gitignore/src/config"
)

// ServerConfig holds server connection settings.
type ServerConfig struct {
	Primary   string `yaml:"primary,omitempty"`
	VerifySSL string `yaml:"verify_ssl,omitempty"`
}

// UpdateConfig holds CLI self-update preferences (checked but not acted on
// today — this project's server does not yet expose /api/autodiscover
// cli_versions; the field is preserved for forward compatibility).
type UpdateConfig struct {
	Auto    string `yaml:"auto,omitempty"`
	Channel string `yaml:"channel,omitempty"`
}

// Config is the full cli.yml document.
type Config struct {
	Server ServerConfig `yaml:"server"`
	Output string       `yaml:"output,omitempty"`
	Color  string       `yaml:"color,omitempty"`
	Lang   string       `yaml:"lang,omitempty"`
	Update UpdateConfig `yaml:"update,omitempty"`
}

// Default returns a fresh Config with sane zero-value defaults.
func Default() *Config {
	return &Config{
		Output: "text",
		Color:  "auto",
		Update: UpdateConfig{Auto: "no", Channel: "stable"},
	}
}

// Load reads the named config profile, creating a default one on first run.
// name is a profile name (bare "test" -> test.yml) or an absolute path.
func Load(name string) (*Config, string, error) {
	path := clipath.ConfigFile(name)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		if saveErr := Save(path, cfg); saveErr != nil {
			return cfg, path, saveErr
		}
		return cfg, path, nil
	}
	if err != nil {
		return nil, path, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, path, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, path, nil
}

// Save writes cfg to path with user-only (0600) permissions, creating
// parent directories first.
func Save(path string, cfg *Config) error {
	if err := clipath.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

// IsValidServerURL reports whether s parses as an absolute http(s) URL.
func IsValidServerURL(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// SaveIfEmptyOrInvalid implements the flag-to-config persistence rule from
// AI.md PART 32 "Flag-to-Config Save Rules": a flag value is written back to
// config only when the current stored value is empty or itself invalid;
// otherwise the flag is used for the current invocation only.
func SaveIfEmptyOrInvalid(current, flagValue string, validate func(string) bool) (result string, shouldSave bool) {
	if flagValue == "" {
		return current, false
	}
	if !validate(flagValue) {
		return current, false
	}
	if current == "" || !validate(current) {
		return flagValue, true
	}
	return flagValue, false
}

// ResolveBool parses a truthy/falsey string using the server's shared
// vocabulary (see src/config.ParseBool), never strconv.ParseBool.
func ResolveBool(s string, defaultVal bool) (bool, error) {
	return srvconfig.ParseBool(s, defaultVal)
}

// IsTruthy re-exports the server's IsTruthy helper for CLI flag/env parsing.
func IsTruthy(s string) bool { return srvconfig.IsTruthy(s) }

// IsFalsy re-exports the server's IsFalsy helper for CLI flag/env parsing.
func IsFalsy(s string) bool { return srvconfig.IsFalsy(s) }

// ResolveServer determines the server base URL using the priority order
// from AI.md PART 32 "Server Address Resolution":
//  1. --server flag (explicit)
//  2. GITIGNORE_SERVER_PRIMARY environment variable
//  3. cli.yml server.primary
//  4. compiled default (none — this project defines no official_site)
func ResolveServer(flagValue string, cfg *Config) (string, error) {
	if flagValue != "" {
		return strings.TrimSuffix(flagValue, "/"), nil
	}
	if env := os.Getenv("GITIGNORE_SERVER_PRIMARY"); env != "" {
		return strings.TrimSuffix(env, "/"), nil
	}
	if cfg != nil && cfg.Server.Primary != "" {
		return strings.TrimSuffix(cfg.Server.Primary, "/"), nil
	}
	return "", fmt.Errorf("no server configured: pass --server URL, set GITIGNORE_SERVER_PRIMARY, or add server.primary to cli.yml")
}
