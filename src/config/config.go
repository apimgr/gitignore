package config

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config represents the complete server configuration
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Web         WebConfig         `yaml:"web"`
	WebRobots   WebRobotsConfig   `yaml:"web_robots"`
	WebSecurity WebSecurityConfig `yaml:"web_security"`
}

// ServerConfig contains server-related settings
type ServerConfig struct {
	Port        string           `yaml:"port"`
	FQDN        string           `yaml:"fqdn"`
	Address     string           `yaml:"address"`
	Mode        string           `yaml:"mode"`
	User        string           `yaml:"user"`
	Group       string           `yaml:"group"`
	PIDFile     bool             `yaml:"pidfile"`
	Branding    BrandingConfig   `yaml:"branding"`
	SEO         SEOConfig        `yaml:"seo"`
	Admin       AdminConfig      `yaml:"admin"`
	SSL         SSLConfig        `yaml:"ssl"`
	Schedule    ScheduleConfig   `yaml:"schedule"`
	RateLimit   RateLimitConfig  `yaml:"rate_limit"`
	Database    DatabaseConfig   `yaml:"database"`
	Logging     LoggingConfig    `yaml:"logging"`
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	UpdateBranch string          `yaml:"update_branch"`
}

// BrandingConfig contains display/branding settings
type BrandingConfig struct {
	Title       string `yaml:"title"`
	Tagline     string `yaml:"tagline"`
	Description string `yaml:"description"`
}

// SEOConfig contains SEO-related settings
type SEOConfig struct {
	Keywords []string `yaml:"keywords"`
}

// AdminConfig contains admin panel settings (NOT credentials — those live in the database)
type AdminConfig struct {
	Email string `yaml:"email"`
}

// SSLConfig contains SSL/TLS settings
type SSLConfig struct {
	Enabled      bool              `yaml:"enabled"`
	LetsEncrypt  LetsEncryptConfig `yaml:"letsencrypt"`
}

// LetsEncryptConfig contains Let's Encrypt settings
type LetsEncryptConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Email     string `yaml:"email"`
	Challenge string `yaml:"challenge"`
}

// ScheduleConfig contains scheduler settings
type ScheduleConfig struct {
	Enabled bool `yaml:"enabled"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	Enabled  bool `yaml:"enabled"`
	Requests int  `yaml:"requests"`
	Window   int  `yaml:"window"`
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	Driver   string `yaml:"driver"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Name     string `yaml:"name,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	SSLMode  string `yaml:"sslmode,omitempty"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	AccessFormat string `yaml:"access_format"`
	Level        string `yaml:"level"`
}

// MaintenanceConfig contains maintenance mode settings
type MaintenanceConfig struct {
	SelfHealing SelfHealingConfig `yaml:"self_healing"`
	Cleanup     CleanupConfig     `yaml:"cleanup"`
	Notify      NotifyConfig      `yaml:"notify"`
}

// SelfHealingConfig contains self-healing settings
type SelfHealingConfig struct {
	Enabled       bool `yaml:"enabled"`
	RetryInterval int  `yaml:"retry_interval"`
	MaxAttempts   int  `yaml:"max_attempts"`
}

// CleanupConfig contains auto-cleanup thresholds
type CleanupConfig struct {
	DiskThreshold    int `yaml:"disk_threshold"`
	LogRetentionDays int `yaml:"log_retention_days"`
	BackupKeepCount  int `yaml:"backup_keep_count"`
}

// NotifyConfig contains notification settings for maintenance events
type NotifyConfig struct {
	OnEnter bool `yaml:"on_enter"`
	OnExit  bool `yaml:"on_exit"`
}

// WebConfig contains frontend/web settings
type WebConfig struct {
	UI   WebUIConfig `yaml:"ui"`
	CORS string      `yaml:"cors"`
}

// WebUIConfig contains web UI settings
type WebUIConfig struct {
	Theme string `yaml:"theme"`
}

// WebRobotsConfig contains robots.txt allow/deny path settings
type WebRobotsConfig struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

// WebSecurityConfig contains security-related web settings
type WebSecurityConfig struct {
	Admin string `yaml:"admin"`
	CORS  string `yaml:"cors"`
}

var (
	current    *Config
	mu         sync.RWMutex
	configPath string
)

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         "",
			FQDN:         "",
			Address:      "[::]",
			Mode:         "production",
			User:         "gitignore",
			Group:        "gitignore",
			PIDFile:      true,
			UpdateBranch: "stable",
			Branding: BrandingConfig{
				Title:       "gitignore",
				Tagline:     "",
				Description: "",
			},
			SEO: SEOConfig{
				Keywords: []string{},
			},
			Admin: AdminConfig{
				Email: "",
			},
			SSL: SSLConfig{
				Enabled: false,
				LetsEncrypt: LetsEncryptConfig{
					Enabled:   false,
					Email:     "",
					Challenge: "http-01",
				},
			},
			Schedule: ScheduleConfig{
				Enabled: true,
			},
			RateLimit: RateLimitConfig{
				Enabled:  true,
				Requests: 120,
				Window:   60,
			},
			Database: DatabaseConfig{
				Driver: "file",
			},
			Logging: LoggingConfig{
				AccessFormat: "apache",
				Level:        "info",
			},
			Maintenance: MaintenanceConfig{
				SelfHealing: SelfHealingConfig{
					Enabled:       true,
					RetryInterval: 30,
					MaxAttempts:   0,
				},
				Cleanup: CleanupConfig{
					DiskThreshold:    90,
					LogRetentionDays: 7,
					BackupKeepCount:  5,
				},
				Notify: NotifyConfig{
					OnEnter: true,
					OnExit:  true,
				},
			},
		},
		Web: WebConfig{
			UI: WebUIConfig{
				Theme: "dark",
			},
			CORS: "*",
		},
		WebRobots: WebRobotsConfig{
			Allow: []string{"/", "/api"},
			Deny:  []string{"/debug"},
		},
		WebSecurity: WebSecurityConfig{
			Admin: "",
			CORS:  "*",
		},
	}
}

// migrateYamlToYml migrates .yaml config files to .yml extension
func migrateYamlToYml(path string) {
	if !strings.HasSuffix(path, ".yml") {
		return
	}
	oldPath := strings.TrimSuffix(path, ".yml") + ".yaml"
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.Rename(oldPath, path); err == nil {
				fmt.Printf("Migrated config file: %s -> %s\n", oldPath, path)
			}
		}
	}
}

// Load loads configuration from a YAML file
func Load(path string) (*Config, error) {
	mu.Lock()
	defer mu.Unlock()

	migrateYamlToYml(path)
	configPath = path

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if cfg.Server.Port == "" {
			cfg.Server.Port = fmt.Sprintf("%d", randomPort())
		}
		if cfg.Server.FQDN == "" {
			cfg.Server.FQDN = detectFQDN()
		}
		if cfg.Server.Admin.Email == "" {
			cfg.Server.Admin.Email = "admin@" + cfg.Server.FQDN
		}
		if cfg.Server.SSL.LetsEncrypt.Email == "" {
			cfg.Server.SSL.LetsEncrypt.Email = cfg.Server.Admin.Email
		}
		if err := saveConfig(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}
		current = cfg
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Fill in computed defaults for existing configs missing these fields
	if cfg.Server.Port == "" {
		cfg.Server.Port = fmt.Sprintf("%d", randomPort())
		_ = saveConfig(cfg, path)
	}
	if cfg.Server.FQDN == "" {
		cfg.Server.FQDN = detectFQDN()
	}

	current = cfg
	return cfg, nil
}

// Get returns the current configuration (thread-safe)
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return DefaultConfig()
	}
	return current
}

// Save persists the current in-memory config to disk
func Save() error {
	mu.Lock()
	defer mu.Unlock()
	if current == nil || configPath == "" {
		return fmt.Errorf("no configuration loaded")
	}
	return saveConfig(current, configPath)
}

// Update applies fn to the current config then saves
func Update(fn func(*Config)) error {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		return fmt.Errorf("no configuration loaded")
	}
	fn(current)
	return saveConfig(current, configPath)
}

func saveConfig(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	content := generateConfigYAML(cfg)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

func generateConfigYAML(cfg *Config) string {
	keywords := "[]"
	if len(cfg.Server.SEO.Keywords) > 0 {
		keywords = "[" + strings.Join(cfg.Server.SEO.Keywords, ", ") + "]"
	}

	dbSection := fmt.Sprintf(`    driver: %s`, cfg.Server.Database.Driver)
	if cfg.Server.Database.Host != "" {
		dbSection += fmt.Sprintf(`
    host: %s
    port: %d
    name: %s
    username: %s`,
			cfg.Server.Database.Host,
			cfg.Server.Database.Port,
			cfg.Server.Database.Name,
			cfg.Server.Database.Username,
		)
	}

	return fmt.Sprintf(`# =============================================================================
# SERVER CONFIGURATION
# =============================================================================

server:
  port: "%s"       # Default: random unused port in 64000-64999 range
  fqdn: "%s"       # Auto-detected from host; set DOMAIN env var to override
  address: "%s"    # [::] = all interfaces IPv4+IPv6
  mode: %s         # production or development
  user: %s
  group: %s
  pidfile: %t

  # Branding & SEO
  branding:
    title: "%s"
    tagline: "%s"
    description: "%s"
  seo:
    keywords: %s

  # Admin panel (credentials stored in database, not here)
  admin:
    email: "%s"

  # SSL/TLS
  ssl:
    enabled: %t
    letsencrypt:
      enabled: %t
      email: "%s"
      challenge: %s

  # Scheduler
  schedule:
    enabled: %t

  # Rate limiting
  rate_limit:
    enabled: %t
    requests: %d  # requests per window
    window: %d    # seconds

  # Database
  database:
%s

  # Logging
  logging:
    access_format: %s
    level: %s

  # Maintenance mode auto-recovery
  maintenance:
    self_healing:
      enabled: %t
      retry_interval: %d  # seconds between retry attempts
      max_attempts: %d    # 0 = unlimited
    cleanup:
      disk_threshold: %d       # start cleanup when disk > N%%
      log_retention_days: %d
      backup_keep_count: %d
    notify:
      on_enter: %t
      on_exit: %t

  update_branch: %s

# =============================================================================
# FRONTEND CONFIGURATION
# =============================================================================

web:
  ui:
    theme: %s
  cors: "%s"
`,
		cfg.Server.Port,
		cfg.Server.FQDN,
		cfg.Server.Address,
		cfg.Server.Mode,
		cfg.Server.User,
		cfg.Server.Group,
		cfg.Server.PIDFile,
		cfg.Server.Branding.Title,
		cfg.Server.Branding.Tagline,
		cfg.Server.Branding.Description,
		keywords,
		cfg.Server.Admin.Email,
		cfg.Server.SSL.Enabled,
		cfg.Server.SSL.LetsEncrypt.Enabled,
		cfg.Server.SSL.LetsEncrypt.Email,
		cfg.Server.SSL.LetsEncrypt.Challenge,
		cfg.Server.Schedule.Enabled,
		cfg.Server.RateLimit.Enabled,
		cfg.Server.RateLimit.Requests,
		cfg.Server.RateLimit.Window,
		dbSection,
		cfg.Server.Logging.AccessFormat,
		cfg.Server.Logging.Level,
		cfg.Server.Maintenance.SelfHealing.Enabled,
		cfg.Server.Maintenance.SelfHealing.RetryInterval,
		cfg.Server.Maintenance.SelfHealing.MaxAttempts,
		cfg.Server.Maintenance.Cleanup.DiskThreshold,
		cfg.Server.Maintenance.Cleanup.LogRetentionDays,
		cfg.Server.Maintenance.Cleanup.BackupKeepCount,
		cfg.Server.Maintenance.Notify.OnEnter,
		cfg.Server.Maintenance.Notify.OnExit,
		cfg.Server.UpdateBranch,
		cfg.Web.UI.Theme,
		cfg.Web.CORS,
	)
}

// randomPort selects a random unused port in the 64000-64999 range
func randomPort() int {
	for attempts := 0; attempts < 100; attempts++ {
		port := 64000 + rand.Intn(1000)
		addr := fmt.Sprintf(":%d", port)
		l, err := net.Listen("tcp", addr)
		if err == nil {
			l.Close()
			return port
		}
	}
	return 64580 // fallback
}

// detectFQDN returns the best available hostname
func detectFQDN() string {
	if domain := os.Getenv("DOMAIN"); domain != "" {
		return domain
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" && !isLoopback(hostname) {
		return hostname
	}
	if hostname := os.Getenv("HOSTNAME"); hostname != "" && !isLoopback(hostname) {
		return hostname
	}
	return "localhost"
}

func isLoopback(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// GetTheme returns the current UI theme
func GetTheme() string {
	return Get().Web.UI.Theme
}

// GetCORS returns the CORS setting
func GetCORS() string {
	cfg := Get()
	if cfg.Web.CORS == "" {
		return "*"
	}
	return cfg.Web.CORS
}

// truthyValues holds the extended set of truthy strings from the spec (case-insensitive)
var truthyValues = map[string]bool{
	"1": true, "y": true, "t": true,
	"yes": true, "true": true, "on": true, "ok": true,
	"enable": true, "enabled": true,
	"yep": true, "yup": true, "yeah": true,
	"aye": true, "si": true, "oui": true, "da": true, "hai": true,
	"affirmative": true, "accept": true, "allow": true, "grant": true,
	"sure": true, "totally": true,
}

// falsyValues holds the extended set of falsy strings from the spec (case-insensitive)
var falsyValues = map[string]bool{
	"0": true, "n": true, "f": true,
	"no": true, "false": true, "off": true,
	"disable": true, "disabled": true,
	"nope": true, "nah": true, "nay": true,
	"nein": true, "non": true, "niet": true, "iie": true, "lie": true,
	"negative": true, "reject": true, "block": true, "revoke": true,
	"deny": true, "never": true, "noway": true,
}

// ParseBool parses a string into a boolean using the extended truthy/falsy
// vocabulary from the spec. Returns the parsed value and nil on success.
// Empty string returns the provided default value. Invalid values return
// an error rather than silently defaulting.
func ParseBool(s string, defaultVal bool) (bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	if s == "" {
		return defaultVal, nil
	}

	if truthyValues[s] {
		return true, nil
	}

	if falsyValues[s] {
		return false, nil
	}

	return false, fmt.Errorf("invalid boolean value: %q", s)
}

// MustParseBool parses a string into a boolean, panicking on invalid value.
// Use only during initialization where invalid config should halt startup.
func MustParseBool(s string, defaultVal bool) bool {
	val, err := ParseBool(s, defaultVal)
	if err != nil {
		panic(err)
	}
	return val
}

// IsTruthy returns true if the string is a truthy value.
// Returns false for empty, invalid, or falsy values (no error).
func IsTruthy(s string) bool {
	return truthyValues[strings.ToLower(strings.TrimSpace(s))]
}

// IsFalsy returns true if the string is a falsy value.
// Returns false for empty, invalid, or truthy values (no error).
func IsFalsy(s string) bool {
	return falsyValues[strings.ToLower(strings.TrimSpace(s))]
}

// IsDebug returns true if debug mode is enabled via the DEBUG environment variable.
func IsDebug() bool {
	return IsTruthy(os.Getenv("DEBUG"))
}
