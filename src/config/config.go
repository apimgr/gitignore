package config

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	BaseURL     string           `yaml:"baseurl"`
	APIVersion  string           `yaml:"api_version"`
	Daemonize   bool             `yaml:"daemonize"`
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
	Metrics     MetricsConfig    `yaml:"metrics"`
	TrustedProxies TrustedProxiesConfig `yaml:"trusted_proxies"`
	Database    DatabaseConfig   `yaml:"database"`
	Logging     LoggingConfig    `yaml:"logging"`
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	Backup      BackupConfig     `yaml:"backup"`
	I18n        I18nConfig       `yaml:"i18n"`
	Tor         TorConfig        `yaml:"tor"`
	GeoIP       GeoIPConfig      `yaml:"geoip"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Update      UpdateConfig     `yaml:"update"`
	Healthz     HealthzConfig    `yaml:"healthz"`
}

// HealthzConfig controls the optional root /healthz alias (AI.md PART 13). The
// canonical health endpoint is always /server/healthz; the root alias mounts
// the same handler and is served only when Root.Enabled is true.
type HealthzConfig struct {
	Root HealthzRootConfig `yaml:"root"`
}

// HealthzRootConfig gates the /healthz root alias.
type HealthzRootConfig struct {
	Enabled bool `yaml:"enabled"`
}

// UpdateConfig controls self-update behavior (AI.md PART 22). Branch is the
// release channel (stable|beta|daily); AutoInstall lets the update_check
// scheduler task install a found update rather than only notifying (default
// off — installing is always an explicit operator decision); DeferDays gates
// the scheduled task to releases at least this many days old.
type UpdateConfig struct {
	Branch      string `yaml:"branch"`
	AutoInstall bool   `yaml:"auto_install"`
	DeferDays   int    `yaml:"defer_days"`
}

// NotificationsConfig contains the operator/visitor notification settings
// (AI.md PART 17). The public WebUI toast settings are visitor-facing; Email
// carries the SMTP + template configuration used for operator alerts.
type NotificationsConfig struct {
	WebUI WebUINotifyConfig `yaml:"webui"`
	Email EmailConfig       `yaml:"email"`
}

// WebUINotifyConfig controls the visitor-facing toast notifications rendered by
// the public frontend (AI.md PART 17 → "Public WebUI Notification System").
type WebUINotifyConfig struct {
	Position string `yaml:"position"`
	Duration int    `yaml:"duration"`
}

// EmailConfig holds SMTP transport plus template email settings (AI.md PART
// 17). Enabled is auto-set at runtime from SMTP availability and is NEVER
// persisted to disk — there is no manual on/off toggle.
type EmailConfig struct {
	Enabled bool              `yaml:"-"`
	SMTP    SMTPConfig        `yaml:"smtp"`
	From    EmailFromConfig   `yaml:"from"`
	ReplyTo string            `yaml:"reply_to"`
	Events  EmailEventsConfig `yaml:"events"`
}

// SMTPConfig holds the SMTP transport settings. All fields can be overridden by
// the matching SMTP_* environment variable (AI.md PART 17 env priority table).
type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// TLS mode: auto, starttls, tls, none.
	TLS string `yaml:"tls"`
}

// EmailFromConfig holds the envelope sender identity. Empty values fall back to
// the app title and no-reply@{fqdn} respectively.
type EmailFromConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// EmailEventsConfig carries the per-event email switches (AI.md PART 17
// "Operator Notifications" matrix). A false value means "log only".
type EmailEventsConfig struct {
	Startup          bool `yaml:"startup"`
	Shutdown         bool `yaml:"shutdown"`
	BackupComplete   bool `yaml:"backup_complete"`
	BackupFailed     bool `yaml:"backup_failed"`
	SSLExpiring      bool `yaml:"ssl_expiring"`
	SSLRenewed       bool `yaml:"ssl_renewed"`
	SSLRenewalFailed bool `yaml:"ssl_renewal_failed"`
	SecurityAlert    bool `yaml:"security_alert"`
	SchedulerError   bool `yaml:"scheduler_error"`
	UpdateAvailable  bool `yaml:"update_available"`
	UpdateInstalled  bool `yaml:"update_installed"`
}

// DefaultNotificationsConfig returns the AI.md PART 17 default notification
// settings. Email starts disabled at runtime and is enabled only when a working
// SMTP server is detected or configured.
func DefaultNotificationsConfig() NotificationsConfig {
	return NotificationsConfig{
		WebUI: WebUINotifyConfig{
			Position: "top-right",
			Duration: 5,
		},
		Email: EmailConfig{
			SMTP: SMTPConfig{
				Host: "",
				Port: 587,
				TLS:  "auto",
			},
			From:    EmailFromConfig{Name: "", Email: ""},
			ReplyTo: "",
			Events: EmailEventsConfig{
				Startup:          false,
				Shutdown:         false,
				BackupComplete:   false,
				BackupFailed:     true,
				SSLExpiring:      true,
				SSLRenewed:       false,
				SSLRenewalFailed: true,
				SecurityAlert:    true,
				SchedulerError:   true,
				UpdateAvailable:  false,
				UpdateInstalled:  true,
			},
		},
	}
}

// ApplySMTPEnv overrides email settings from SMTP_* environment variables.
// Env vars take precedence over config-file values (AI.md PART 17), which makes
// containerized deployments configurable without editing server.yml.
func ApplySMTPEnv(cfg *Config) {
	e := &cfg.Server.Notifications.Email
	if v := os.Getenv("SMTP_HOST"); v != "" {
		e.SMTP.Host = v
	}
	if v := os.Getenv("SMTP_PORT"); v != "" {
		if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && p > 0 {
			e.SMTP.Port = p
		}
	}
	if v := os.Getenv("SMTP_USERNAME"); v != "" {
		e.SMTP.Username = v
	}
	if v := os.Getenv("SMTP_PASSWORD"); v != "" {
		e.SMTP.Password = v
	}
	if v := os.Getenv("SMTP_TLS"); v != "" {
		e.SMTP.TLS = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("SMTP_FROM_NAME"); v != "" {
		e.From.Name = v
	}
	if v := os.Getenv("SMTP_FROM_EMAIL"); v != "" {
		e.From.Email = v
	}
}

// TorConfig contains Tor hidden service settings (AI.md PART 31). These fields
// are pure data with no dependency on the Tor controller library, so both the
// server and client binaries can embed them without pulling in bine. The tor
// package consumes this struct and owns all controller logic. The feature is
// config-driven: an external "tor" binary is launched only when found and the
// server never fails to start if Tor is unavailable.
type TorConfig struct {
	// Binary is the path to the external tor executable. Empty means
	// auto-detect from PATH and common install locations.
	Binary string `yaml:"binary" json:"binary"`
	// UseNetwork enables an outbound Tor SOCKS dialer in addition to the hidden
	// service. When false the service is hidden-service-only (SocksPort 0).
	UseNetwork bool `yaml:"use_network" json:"use_network"`
	// MaxCircuits caps the number of concurrent circuits.
	MaxCircuits int `yaml:"max_circuits" json:"max_circuits"`
	// CircuitTimeout is the per-circuit build timeout in seconds.
	CircuitTimeout int `yaml:"circuit_timeout" json:"circuit_timeout"`
	// BootstrapTimeout is the maximum time in seconds to wait for the Tor
	// network bootstrap to complete.
	BootstrapTimeout int `yaml:"bootstrap_timeout" json:"bootstrap_timeout"`
	// SafeLogging scrubs sensitive values from Tor's own logs when true.
	SafeLogging bool `yaml:"safe_logging" json:"safe_logging"`
	// MaxStreamsPerCircuit caps streams multiplexed onto a single circuit.
	MaxStreamsPerCircuit int `yaml:"max_streams_per_circuit" json:"max_streams_per_circuit"`
	// CloseCircuitOnStreamLimit closes a circuit that hits the stream limit.
	CloseCircuitOnStreamLimit bool `yaml:"close_circuit_on_stream_limit" json:"close_circuit_on_stream_limit"`
	// BandwidthRate is the sustained bandwidth cap (e.g. "1 MB").
	BandwidthRate string `yaml:"bandwidth_rate" json:"bandwidth_rate"`
	// BandwidthBurst is the burst bandwidth cap (e.g. "2 MB").
	BandwidthBurst string `yaml:"bandwidth_burst" json:"bandwidth_burst"`
	// MaxMonthlyBandwidth enables Tor accounting when set (e.g. "100 GB"); an
	// empty value or "unlimited" disables accounting.
	MaxMonthlyBandwidth string `yaml:"max_monthly_bandwidth" json:"max_monthly_bandwidth"`
	// NumIntroPoints is the number of introduction points for the service.
	NumIntroPoints int `yaml:"num_intro_points" json:"num_intro_points"`
	// VirtualPort is the port exposed on the .onion address (typically 80).
	VirtualPort int `yaml:"virtual_port" json:"virtual_port"`
}

// DefaultTorConfig returns the AI.md PART 31 default Tor settings.
func DefaultTorConfig() TorConfig {
	return TorConfig{
		Binary:                    "",
		UseNetwork:                false,
		MaxCircuits:               32,
		CircuitTimeout:            60,
		BootstrapTimeout:          180,
		SafeLogging:               true,
		MaxStreamsPerCircuit:      100,
		CloseCircuitOnStreamLimit: true,
		BandwidthRate:             "1 MB",
		BandwidthBurst:            "2 MB",
		MaxMonthlyBandwidth:       "100 GB",
		NumIntroPoints:            3,
		VirtualPort:               80,
	}
}

// GeoIPConfig contains GeoIP settings (AI.md PART 19). Databases are never
// embedded — they are downloaded on first run and refreshed by the scheduler's
// geoip_update task. GeoIP is a risk signal only: country blocking never
// replaces authentication, and every field is pure data with no dependency on
// the MMDB reader library, so the server starts even when GeoIP is unavailable.
type GeoIPConfig struct {
	Enabled        bool                 `yaml:"enabled"`
	Dir            string               `yaml:"dir"`
	DenyCountries  []string             `yaml:"deny_countries"`
	AllowCountries []string             `yaml:"allow_countries"`
	Databases      GeoIPDatabasesConfig `yaml:"databases"`
}

// GeoIPDatabasesConfig toggles which MMDB databases are downloaded and queried
// (AI.md PART 19 "Database Sources"). WHOIS is not a separate file — it joins
// the ASN and Country databases at query time.
type GeoIPDatabasesConfig struct {
	ASN     bool `yaml:"asn"`
	Country bool `yaml:"country"`
	City    bool `yaml:"city"`
	Whois   bool `yaml:"whois"`
}

// DefaultGeoIPConfig returns the AI.md PART 19 default GeoIP settings. Country
// blocking is off by default (both lists empty); all four database toggles are
// enabled. Dir is empty so the manager derives {data_dir}/security/geoip.
func DefaultGeoIPConfig() GeoIPConfig {
	return GeoIPConfig{
		Enabled:        true,
		Dir:            "",
		DenyCountries:  []string{},
		AllowCountries: []string{},
		Databases: GeoIPDatabasesConfig{
			ASN:     true,
			Country: true,
			City:    true,
			Whois:   true,
		},
	}
}

// I18nConfig contains internationalization settings (AI.md PART 30). The set
// of available languages is compiled into the binary via go:embed; these
// fields select the startup default, the missing-key fallback locale, and the
// language cookie's name and lifetime.
type I18nConfig struct {
	Enabled            bool     `yaml:"enabled"`
	DefaultLanguage    string   `yaml:"default_language"`
	FallbackLanguage   string   `yaml:"fallback_language"`
	AvailableLanguages []string `yaml:"available_languages"`
	CookieName         string   `yaml:"cookie_name"`
	CookieMaxAge       string   `yaml:"cookie_max_age"`
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

// LetsEncryptConfig contains Let's Encrypt settings. DNSProvider selects a
// DNS-01 provider (AI.md PART 15); its credentials live in DNSCredentials,
// encrypted at rest.
type LetsEncryptConfig struct {
	Enabled        bool                 `yaml:"enabled"`
	Email          string               `yaml:"email"`
	Challenge      string               `yaml:"challenge"`
	DNSProvider    string               `yaml:"dns_provider"`
	DNSCredentials DNSCredentialsConfig `yaml:"dns_credentials"`
}

// DNSCredentialsConfig holds DNS-01 provider credentials for the ACME dns-01
// challenge (AI.md PART 15). Raw provider secrets are NEVER written to the
// generated plaintext config: CredentialsEncrypted holds an AES-256-GCM
// ciphertext of the provider credential JSON, decrypted only in memory when a
// certificate is requested.
type DNSCredentialsConfig struct {
	Provider             string `yaml:"provider"`
	CredentialsEncrypted string `yaml:"credentials_encrypted"`
	ValidatedAt          string `yaml:"validated_at"`
}

// ScheduleConfig contains scheduler settings (AI.md PART 18). The scheduler is
// always running; Enabled is retained for backward compatibility but no longer
// gates startup. Per-task enable/disable and run history live in server.db.
type ScheduleConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Timezone      string `yaml:"timezone"`
	CatchUpWindow string `yaml:"catch_up_window"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	Enabled  bool `yaml:"enabled"`
	Requests int  `yaml:"requests"`
	Window   int  `yaml:"window"`
}

// MetricsConfig controls the Prometheus /metrics endpoint (AI.md PART 20).
// The endpoint is INTERNAL ONLY — operators firewall it externally. Token is
// an optional bearer credential added on top of network isolation; empty means
// no application-layer auth. Duration/size histogram buckets are configurable
// so operators can tune bucketing to their latency profile.
type MetricsConfig struct {
	Enabled         bool      `yaml:"enabled"`
	Endpoint        string    `yaml:"endpoint"`
	IncludeSystem   bool      `yaml:"include_system"`
	IncludeRuntime  bool      `yaml:"include_runtime"`
	Token           string    `yaml:"token"`
	DurationBuckets []float64 `yaml:"duration_buckets"`
	SizeBuckets     []float64 `yaml:"size_buckets"`
}

// TrustedProxiesConfig lists upstream reverse proxies whose X-Forwarded-*
// headers may be trusted. Private ranges and the listen /24 are always
// trusted; Additional adds public IPs/CIDRs/DNS names (AI.md PART 12).
type TrustedProxiesConfig struct {
	Additional []string `yaml:"additional"`
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

// BackupConfig contains backup settings (AI.md PART 21).
type BackupConfig struct {
	Encryption BackupEncryptionConfig `yaml:"encryption"`
}

// BackupEncryptionConfig controls backup-at-rest encryption. The password is
// NEVER stored — it is prompted interactively when Enabled is true (AI.md
// PART 21 → "Backup Encryption").
type BackupEncryptionConfig struct {
	Enabled bool `yaml:"enabled"`
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
			BaseURL:      "/",
			APIVersion:   "v1",
			Daemonize:    false,
			Mode:         "production",
			User:         "gitignore",
			Group:        "gitignore",
			PIDFile:      true,
			Update: UpdateConfig{
				Branch:      "stable",
				AutoInstall: false,
				DeferDays:   0,
			},
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
			Healthz: HealthzConfig{
				Root: HealthzRootConfig{
					Enabled: false,
				},
			},
			Schedule: ScheduleConfig{
				Enabled:       true,
				Timezone:      "America/New_York",
				CatchUpWindow: "1h",
			},
			RateLimit: RateLimitConfig{
				Enabled:  true,
				Requests: 120,
				Window:   60,
			},
			Metrics: MetricsConfig{
				Enabled:         true,
				Endpoint:        "/metrics",
				IncludeSystem:   true,
				IncludeRuntime:  true,
				Token:           "",
				DurationBuckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
				SizeBuckets:     []float64{100, 1000, 10000, 100000, 1000000, 10000000},
			},
			TrustedProxies: TrustedProxiesConfig{
				Additional: []string{},
			},
			Database: DatabaseConfig{
				Driver: "sqlite",
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
			Backup: BackupConfig{
				Encryption: BackupEncryptionConfig{
					Enabled: false,
				},
			},
			I18n: I18nConfig{
				Enabled:            true,
				DefaultLanguage:    "en",
				FallbackLanguage:   "en",
				AvailableLanguages: []string{"en", "es", "zh", "fr", "ar", "de", "ja"},
				CookieName:         "lang",
				CookieMaxAge:       "365d",
			},
			Tor:           DefaultTorConfig(),
			GeoIP:         DefaultGeoIPConfig(),
			Notifications: DefaultNotificationsConfig(),
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

	base := fmt.Sprintf(`# =============================================================================
# SERVER CONFIGURATION
# =============================================================================

server:
  # Default: random unused port in 64000-64999 range
  port: "%s"
  # Auto-detected from host; set DOMAIN env var to override
  fqdn: "%s"
  # [::] = all interfaces IPv4+IPv6
  address: "%s"
  # production or development
  mode: %s
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
  # Cert lookup order (AI.md PART 15): local certs are checked BEFORE requesting
  # a new Let's Encrypt cert.
  #   /etc/letsencrypt/live/{fqdn}/   -> system manages (app uses, never renews)
  #   ssl/letsencrypt/{fqdn}/         -> app manages (auto-renew 7 days before expiry)
  #   ssl/local/{fqdn}/               -> user manages (no auto-renew)
  # Overlay addresses (.onion, .i2p, .exit) always use a self-signed certificate.
  ssl:
    enabled: %t
    letsencrypt:
      enabled: %t
      email: "%s"
      # http-01, tls-alpn-01, dns-01
      challenge: %s
      # DNS-01 provider (cloudflare, route53, digitalocean, godaddy, namecheap,
      # rfc2136, ...); required only when challenge is dns-01
      dns_provider: "%s"
      # DNS provider credentials are stored encrypted at rest (AES-256-GCM).
      # Never place raw provider secrets in this file; the server writes the
      # encrypted blob and validation timestamp here after you configure the
      # provider via the admin interface.
      dns_credentials:
        provider: ""
        credentials_encrypted: ""
        validated_at: ""

  # Optional root /healthz alias for the canonical /server/healthz endpoint
  healthz:
    root:
      enabled: %t

  # Scheduler (always running; enabled kept for compatibility)
  schedule:
    enabled: %t
    # IANA timezone for cron schedules
    timezone: %s
    # run missed tasks on restart if within this window
    catch_up_window: %s

  # Rate limiting
  rate_limit:
    enabled: %t
    # requests per window
    requests: %d
    # seconds
    window: %d

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
      # seconds between retry attempts
      retry_interval: %d
      # 0 = unlimited
      max_attempts: %d
    cleanup:
      # start cleanup when disk > N%%
      disk_threshold: %d
      log_retention_days: %d
      backup_keep_count: %d
    notify:
      on_enter: %t
      on_exit: %t

  # Internationalization (AI.md PART 30)
  i18n:
    enabled: %t
    default_language: %s
    available_languages: [%s]
    fallback_language: %s
    cookie_name: %s
    # 1 year
    cookie_max_age: %s

  # Tor hidden service (AI.md PART 31)
  # Hidden service is auto-enabled whenever the tor binary is found; there is
  # no enable/disable toggle. The server owns the tor process lifecycle.
  tor:
    # Path to the tor binary (empty = auto-detect from PATH)
    binary: "%s"
    # Route the server's outbound requests through Tor
    use_network: %t
    # Maximum concurrent circuits to keep open
    max_circuits: %d
    # Circuit build timeout in seconds
    circuit_timeout: %d
    # Seconds to wait for the Tor network bootstrap to complete
    bootstrap_timeout: %d
    # Scrub sensitive values from Tor's own logs
    safe_logging: %t
    # Maximum concurrent streams per circuit
    max_streams_per_circuit: %d
    # Close a circuit when it exceeds the stream limit
    close_circuit_on_stream_limit: %t
    # Sustained bandwidth cap
    bandwidth_rate: "%s"
    # Burst bandwidth cap
    bandwidth_burst: "%s"
    # Monthly bandwidth cap ("unlimited" or empty disables accounting)
    max_monthly_bandwidth: "%s"
    # Number of introduction points (3-10)
    num_intro_points: %d
    # Virtual port exposed on the .onion address
    virtual_port: %d

  # GeoIP (AI.md PART 19)
  # Databases are never embedded; they download on first run and refresh weekly
  # via the scheduler. GeoIP is a risk signal only — country blocking never
  # replaces authentication, and a missing database fails open.
  geoip:
    enabled: true
    # Directory for downloaded MMDB files (empty = {data_dir}/security/geoip)
    dir: ""
    # ISO 3166-1 alpha-2 codes. Set ONE list or NEITHER; allow wins if both set.
    deny_countries: []
    allow_countries: []
    databases:
      asn: true
      country: true
      city: true
      whois: true

  update:
    # Release channel: stable | beta | daily (also settable via --update branch)
    branch: %s
    # Auto-install updates found by the update_check task
    # Default OFF: the task only notifies; installing is always an explicit operator decision
    auto_install: %t
    # Defer window in days (0-365): a release is only eligible once it is this many days old
    defer_days: %d

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
		cfg.Server.SSL.LetsEncrypt.DNSProvider,
		cfg.Server.Healthz.Root.Enabled,
		cfg.Server.Schedule.Enabled,
		cfg.Server.Schedule.Timezone,
		cfg.Server.Schedule.CatchUpWindow,
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
		cfg.Server.I18n.Enabled,
		cfg.Server.I18n.DefaultLanguage,
		strings.Join(cfg.Server.I18n.AvailableLanguages, ", "),
		cfg.Server.I18n.FallbackLanguage,
		cfg.Server.I18n.CookieName,
		cfg.Server.I18n.CookieMaxAge,
		cfg.Server.Tor.Binary,
		cfg.Server.Tor.UseNetwork,
		cfg.Server.Tor.MaxCircuits,
		cfg.Server.Tor.CircuitTimeout,
		cfg.Server.Tor.BootstrapTimeout,
		cfg.Server.Tor.SafeLogging,
		cfg.Server.Tor.MaxStreamsPerCircuit,
		cfg.Server.Tor.CloseCircuitOnStreamLimit,
		cfg.Server.Tor.BandwidthRate,
		cfg.Server.Tor.BandwidthBurst,
		cfg.Server.Tor.MaxMonthlyBandwidth,
		cfg.Server.Tor.NumIntroPoints,
		cfg.Server.Tor.VirtualPort,
		cfg.Server.Update.Branch,
		cfg.Server.Update.AutoInstall,
		cfg.Server.Update.DeferDays,
		cfg.Web.UI.Theme,
		cfg.Web.CORS,
	)

	// The notification block lives under server:, so inject it just before the
	// update block rather than reflowing the large Sprintf argument list.
	return strings.Replace(base, "  update:", generateNotificationsYAML(cfg)+"  update:", 1)
}

// generateNotificationsYAML renders the server.notifications block (AI.md PART
// 17). Email.Enabled is intentionally omitted: it is auto-set at runtime from
// SMTP availability and never persisted.
func generateNotificationsYAML(cfg *Config) string {
	n := cfg.Server.Notifications
	e := n.Email
	return fmt.Sprintf(`  # Notifications (AI.md PART 17)
  notifications:
    # Public WebUI toasts (client-side, visitor-facing only)
    webui:
      # top-right, top-left, bottom-right, bottom-left
      position: %s
      # seconds (0 = manual dismiss)
      duration: %d
    # Email notifications; enabled is auto-set from SMTP availability.
    # All SMTP settings can be overridden via SMTP_* env vars.
    email:
      smtp:
        # If empty: autodetect local SMTP on startup. If set: test connection.
        host: "%s"
        port: %d
        username: "%s"
        password: "%s"
        # TLS mode: auto, starttls, tls, none
        tls: %s
      from:
        # Default: app title
        name: "%s"
        # Default: no-reply@{fqdn}
        email: "%s"
      # Reply-To address (omitted from emails when empty)
      reply_to: "%s"
      # Per-event email switches (override defaults)
      events:
        startup: %t
        shutdown: %t
        backup_complete: %t
        backup_failed: %t
        ssl_expiring: %t
        ssl_renewed: %t
        ssl_renewal_failed: %t
        security_alert: %t
        scheduler_error: %t
        update_available: %t
        update_installed: %t

`,
		n.WebUI.Position,
		n.WebUI.Duration,
		e.SMTP.Host,
		e.SMTP.Port,
		e.SMTP.Username,
		e.SMTP.Password,
		e.SMTP.TLS,
		e.From.Name,
		e.From.Email,
		e.ReplyTo,
		e.Events.Startup,
		e.Events.Shutdown,
		e.Events.BackupComplete,
		e.Events.BackupFailed,
		e.Events.SSLExpiring,
		e.Events.SSLRenewed,
		e.Events.SSLRenewalFailed,
		e.Events.SecurityAlert,
		e.Events.SchedulerError,
		e.Events.UpdateAvailable,
		e.Events.UpdateInstalled,
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
