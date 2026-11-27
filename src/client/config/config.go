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
	"github.com/apimgr/gitignore/src/common/i18n"
	srvconfig "github.com/apimgr/gitignore/src/config"
)

// ServerConfig holds server connection settings.
type ServerConfig struct {
	Primary   string `yaml:"primary,omitempty"`
	VerifySSL string `yaml:"verify_ssl,omitempty"`
}

// AuthConfig stores an optional API token in the owner-only CLI config.
type AuthConfig struct {
	Token     string `yaml:"token,omitempty"`
	TokenFile string `yaml:"token_file,omitempty"`
}

// UpdateConfig holds the CLI self-update preferences. The settings are
// preserved across profile reads and writes while server release feeds are
// not yet configured for this project.
type UpdateConfig struct {
	Auto    string `yaml:"auto,omitempty"`
	Channel string `yaml:"channel,omitempty"`
}

// DisplayConfig selects the interactive UI mode. Auto preserves environment
// based selection; gui and tui are explicit user preferences.
type DisplayConfig struct {
	Mode string `yaml:"mode,omitempty"`
}

// Config is the full cli.yml document.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Auth    AuthConfig    `yaml:"auth,omitempty"`
	Output  string        `yaml:"output,omitempty"`
	Color   string        `yaml:"color,omitempty"`
	Lang    string        `yaml:"lang,omitempty"`
	Update  UpdateConfig  `yaml:"update,omitempty"`
	Display DisplayConfig `yaml:"display,omitempty"`
}

// Default returns a fresh Config with sane zero-value defaults.
func Default() *Config {
	return &Config{
		Output:  "text",
		Color:   "auto",
		Update:  UpdateConfig{Auto: "no", Channel: "stable"},
		Display: DisplayConfig{Mode: "auto"},
	}
}

// Load reads the named config profile, creating a default one on first run.
// name is a profile name (bare "test" -> test.yml) or an absolute path.
func Load(name string, languages ...string) (*Config, string, error) {
	lang := configLanguage(languages)
	path := clipath.ConfigFile(name)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		if saveErr := Save(path, cfg, lang); saveErr != nil {
			return cfg, path, saveErr
		}
		return cfg, path, nil
	}
	if err != nil {
		return nil, path, fmt.Errorf("%s: %w", i18n.TranslateFormat(lang, "cli.client.config.read_failed", map[string]string{"path": path}), err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, path, fmt.Errorf("%s: %w", i18n.TranslateFormat(lang, "cli.client.config.parse_failed", map[string]string{"path": path}), err)
	}
	if cfg.Display.Mode == "" {
		cfg.Display.Mode = "auto"
	}
	if !IsValidDisplayMode(cfg.Display.Mode) {
		return nil, path, fmt.Errorf("%s", i18n.Translate(lang, "cli.client.config.invalid_display_mode"))
	}
	return cfg, path, nil
}

// IsValidDisplayMode reports whether mode is an allowed UI preference.
func IsValidDisplayMode(mode string) bool {
	return mode == "auto" || mode == "gui" || mode == "tui"
}

// Save writes cfg to path with user-only (0600) permissions, creating
// parent directories first.
func Save(path string, cfg *Config, languages ...string) error {
	lang := configLanguage(languages)
	if err := clipath.EnsureDirs(); err != nil {
		return fmt.Errorf("%s: %w", i18n.Translate(lang, "cli.client.config.ensure_directories_failed"), err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.Translate(lang, "cli.client.config.encode_failed"), err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("%s: %w", i18n.TranslateFormat(lang, "cli.client.config.write_failed", map[string]string{"path": path}), err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("%s: %w", i18n.TranslateFormat(lang, "cli.client.config.permissions_failed", map[string]string{"path": path}), err)
	}
	return nil
}

func configLanguage(languages []string) string {
	if len(languages) > 0 {
		return i18n.ResolveCLILang(languages[0], "")
	}
	return i18n.ResolveCLILang("", "")
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
