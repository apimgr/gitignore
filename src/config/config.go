package config

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"math/rand"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	apppath "github.com/apimgr/gitignore/src/path"
	"gopkg.in/yaml.v3"
)

var configLanguage struct {
	sync.RWMutex
	value string
}

// SetLanguage selects the language used by configuration diagnostics.
func SetLanguage(language string) {
	configLanguage.Lock()
	configLanguage.value = language
	configLanguage.Unlock()
}

func configText(key string, values ...interface{}) string {
	configLanguage.RLock()
	language := configLanguage.value
	configLanguage.RUnlock()
	if language == "" {
		language = i18n.DefaultLang
	}
	return i18n.TranslateFormat(language, key, values...)
}

// Config represents the complete server configuration
type Config struct {
	Server ServerConfig `yaml:"server"`
	Web    WebConfig    `yaml:"web"`
	Data   DataConfig   `yaml:"data"`
	CLI    CLIConfig    `yaml:"cli"`
	// LegacySecrets stages imported database values without serializing them.
	LegacySecrets map[string]string `yaml:"-"`
}

// ServerConfig contains server-related settings
type ServerConfig struct {
	Port           string               `yaml:"port"`
	Token          string               `yaml:"-"`
	FQDN           string               `yaml:"fqdn"`
	Address        string               `yaml:"address"`
	BaseURL        string               `yaml:"baseurl"`
	APIVersion     string               `yaml:"api_version"`
	Daemonize      bool                 `yaml:"daemonize"`
	Mode           string               `yaml:"mode"`
	Debug          DebugConfig          `yaml:"debug"`
	PIDFile        bool                 `yaml:"pidfile"`
	Branding       BrandingConfig       `yaml:"branding"`
	SEO            SEOConfig            `yaml:"seo"`
	Admin          AdminConfig          `yaml:"admin"`
	SSL            SSLConfig            `yaml:"ssl"`
	Schedule       ScheduleConfig       `yaml:"scheduler"`
	RateLimit      RateLimitConfig      `yaml:"rate_limit"`
	Metrics        MetricsConfig        `yaml:"metrics"`
	TrustedProxies TrustedProxiesConfig `yaml:"trusted_proxies"`
	CORS           CORSConfig           `yaml:"cors"`
	Database       DatabaseConfig       `yaml:"database"`
	Cache          CacheConfig          `yaml:"cache"`
	Logging        LoggingConfig        `yaml:"logs"`
	Maintenance    MaintenanceConfig    `yaml:"maintenance"`
	Backup         BackupConfig         `yaml:"backup"`
	Restore        RestoreConfig        `yaml:"restore"`
	I18n           I18nConfig           `yaml:"i18n"`
	Tor            TorConfig            `yaml:"tor"`
	I2P            I2PConfig            `yaml:"i2p"`
	GeoIP          GeoIPConfig          `yaml:"geoip"`
	Notifications  NotificationsConfig  `yaml:"notifications"`
	Update         UpdateConfig         `yaml:"update"`
	Healthz        HealthzConfig        `yaml:"healthz"`
	Contact        ContactConfig        `yaml:"contact"`
	Pages          PagesConfig          `yaml:"pages"`
	Privacy        PrivacyConfig        `yaml:"privacy"`
	Compliance     ComplianceConfig     `yaml:"compliance"`
	Tracking       TrackingConfig       `yaml:"tracking"`
	Security       ServerSecurityConfig `yaml:"security"`
	Tokens         OwnerTokensConfig    `yaml:"tokens"`
}

// DebugConfig controls diagnostics that are available only while the
// independent debug flag is enabled.
type DebugConfig struct {
	Pprof                bool   `yaml:"pprof"`
	LogQueries           bool   `yaml:"log_queries"`
	LogCache             bool   `yaml:"log_cache"`
	LogBodies            bool   `yaml:"log_bodies"`
	MaxBodyLogSize       string `yaml:"max_body_log_size"`
	BlockProfileRate     int    `yaml:"block_profile_rate"`
	MutexProfileFraction int    `yaml:"mutex_profile_fraction"`
	RuntimeEndpoints     bool   `yaml:"runtime_endpoints"`
}

// UnmarshalYAML accepts the canonical paths and reads the two retired names so
// existing installations do not lose their configured scheduler or log level.
func (s *ServerConfig) UnmarshalYAML(node *yaml.Node) error {
	type canonicalServerConfig ServerConfig
	canonical := canonicalServerConfig(*s)
	if err := node.Decode(&canonical); err != nil {
		return err
	}
	*s = ServerConfig(canonical)

	if mappingValue(node, "scheduler") == nil {
		if legacy := mappingValue(node, "schedule"); legacy != nil {
			if err := legacy.Decode(&s.Schedule); err != nil {
				return err
			}
		}
	}
	if legacy := mappingValue(node, "logging"); legacy != nil {
		if mappingValue(node, "logs") == nil {
			var old struct {
				AccessFormat string `yaml:"access_format"`
				Level        string `yaml:"level"`
			}
			if err := legacy.Decode(&old); err != nil {
				return err
			}
			if old.AccessFormat != "" {
				s.Logging.Access.Format = old.AccessFormat
			}
			if old.Level != "" {
				s.Logging.Level = old.Level
			}
		}
	}
	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// OwnerTokensConfig defines owner-token limits and the owner-cookie lifetime.
type OwnerTokensConfig struct {
	MaxLinked     int    `yaml:"max_linked"`
	CookieMaxAge  string `yaml:"cookie_max_age"`
	IdlePurgeDays int    `yaml:"idle_purge_days"`
}

// I2PConfig contains the opt-in eepsite settings (AI.md PART 31.2).
type I2PConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Expose           bool   `yaml:"expose"`
	Binary           string `yaml:"binary"`
	SAMAddress       string `yaml:"sam_address"`
	VirtualPort      int    `yaml:"virtual_port"`
	InboundLength    int    `yaml:"inbound_length"`
	OutboundLength   int    `yaml:"outbound_length"`
	InboundQuantity  int    `yaml:"inbound_quantity"`
	OutboundQuantity int    `yaml:"outbound_quantity"`
	SignatureType    int    `yaml:"signature_type"`
	BootstrapTimeout int    `yaml:"bootstrap_timeout"`
}

// DefaultI2PConfig computes the opt-in default from the runtime environment.
func DefaultI2PConfig() I2PConfig {
	return I2PConfig{
		Enabled:          apppath.IsRunningInContainer(),
		Expose:           false,
		SAMAddress:       "127.0.0.1:7656",
		VirtualPort:      80,
		InboundLength:    3,
		OutboundLength:   3,
		InboundQuantity:  5,
		OutboundQuantity: 5,
		SignatureType:    7,
		BootstrapTimeout: 300,
	}
}

type PrivacyConfig struct {
	Data       PrivacyDataConfig       `yaml:"data"`
	Retention  PrivacyRetentionConfig  `yaml:"retention"`
	Consent    PrivacyConsentConfig    `yaml:"consent"`
	Cookies    PrivacyCookiesConfig    `yaml:"cookies"`
	ThirdParty PrivacyThirdPartyConfig `yaml:"third_party"`
}

type PrivacyDataConfig struct {
	Sold           bool                   `yaml:"sold"`
	StoredOnServer *bool                  `yaml:"stored_on_server"`
	Sharing        []PrivacySharingConfig `yaml:"sharing"`
}

type PrivacySharingConfig struct {
	Condition string `yaml:"condition"`
	When      string `yaml:"when"`
	Data      string `yaml:"data"`
}

type PrivacyRetentionConfig struct {
	Period            string `yaml:"period"`
	ExportAvailable   bool   `yaml:"export_available"`
	DeletionAvailable bool   `yaml:"deletion_available"`
}

type PrivacyConsentConfig struct {
	ShowUntilAcknowledged bool   `yaml:"show_until_acknowledged"`
	Message               string `yaml:"message"`
	MessageIfSold         string `yaml:"message_if_sold"`
	Policy                struct {
		Text    string `yaml:"text"`
		URL     string `yaml:"url"`
		Version string `yaml:"version"`
	} `yaml:"policy"`
	Buttons struct {
		Decline string `yaml:"decline"`
		Accept  string `yaml:"accept"`
	} `yaml:"buttons"`
	Position        string `yaml:"position"`
	ShowPreferences bool   `yaml:"show_preferences"`
	PreferencesText string `yaml:"preferences_text"`
}

type PrivacyCookiesConfig struct {
	Essential struct {
		Enabled     bool   `yaml:"enabled"`
		Description string `yaml:"description"`
	} `yaml:"essential"`
	Preferences struct {
		Enabled     bool   `yaml:"enabled"`
		Description string `yaml:"description"`
	} `yaml:"preferences"`
	Analytics struct {
		Enabled                  bool   `yaml:"enabled"`
		Description              string `yaml:"description"`
		DescriptionSuffixNotSold string `yaml:"description_suffix_not_sold"`
		DescriptionSuffixSold    string `yaml:"description_suffix_sold"`
	} `yaml:"analytics"`
}

type PrivacyThirdPartyConfig struct {
	Services []PrivacyThirdPartyServiceConfig `yaml:"services"`
}

type PrivacyThirdPartyServiceConfig struct {
	Name      string `yaml:"name"`
	Purpose   string `yaml:"purpose"`
	DataSent  string `yaml:"data_sent"`
	PolicyURL string `yaml:"policy_url"`
}

func DefaultPrivacyConfig() PrivacyConfig {
	privacy := PrivacyConfig{
		Data: PrivacyDataConfig{
			StoredOnServer: boolPointer(true),
			Sharing: []PrivacySharingConfig{
				{Condition: "analytics", When: "Tracking configured (server.tracking.type set) AND user consents", Data: "Anonymized: page views, browser type, country"},
				{Condition: "email", When: "SMTP configured for sending emails", Data: "Email address, message content"},
				{Condition: "user_initiated", When: "User explicitly shares content (social buttons, exports)", Data: "Whatever user chooses to share"},
			},
		},
		Retention: PrivacyRetentionConfig{
			Period:            "Resources you create are kept until they expire or you delete them with your owner token. Preferences are stored only in cookies in your browser. Operational logs are kept for up to 30 days. Anonymized analytics data may be retained for up to 12 months.",
			ExportAvailable:   true,
			DeletionAvailable: true,
		},
		ThirdParty: PrivacyThirdPartyConfig{Services: []PrivacyThirdPartyServiceConfig{}},
	}
	privacy.Consent.ShowUntilAcknowledged = true
	privacy.Consent.Message = "In accordance with the EU GDPR law this message is being displayed. We use cookies for essential site functionality and, with your consent, for preferences and analytics. Your data is stored on our servers and is never sold."
	privacy.Consent.MessageIfSold = "In accordance with the EU GDPR law this message is being displayed. We use cookies for essential site functionality and, with your consent, for preferences and analytics. Your data may be shared with or sold to third parties as described in our Privacy Policy."
	privacy.Consent.Policy.Text, privacy.Consent.Policy.URL = "Privacy Policy", "/server/privacy"
	privacy.Consent.Buttons.Decline, privacy.Consent.Buttons.Accept = "Decline", "I Agree"
	privacy.Consent.Position, privacy.Consent.PreferencesText = "bottom", "Manage Preferences"
	privacy.Consent.ShowPreferences = true
	privacy.Cookies.Essential.Enabled = true
	privacy.Cookies.Essential.Description = "Required for the site to function. Includes security tokens (CSRF) and site preferences. These cookies are strictly necessary and cannot be disabled."
	privacy.Cookies.Preferences.Enabled = true
	privacy.Cookies.Preferences.Description = "Remember your settings such as theme (dark/light), language, and UI preferences. Disabling will reset to defaults on each visit."
	privacy.Cookies.Analytics.Enabled = true
	privacy.Cookies.Analytics.Description = "Help us understand how visitors use the site to improve the experience."
	privacy.Cookies.Analytics.DescriptionSuffixNotSold = "Analytics data is anonymized and never sold."
	privacy.Cookies.Analytics.DescriptionSuffixSold = "Analytics data may be shared with third parties."
	return privacy
}

type ComplianceConfig struct {
	Enabled  bool `yaml:"enabled"`
	GDPR     bool `yaml:"gdpr"`
	CCPA     bool `yaml:"ccpa"`
	HIPAA    bool `yaml:"hipaa"`
	SOC2     bool `yaml:"soc2"`
	PCIDSS   bool `yaml:"pci_dss"`
	ISO27001 bool `yaml:"iso27001"`
	FedRAMP  bool `yaml:"fedramp"`
	LGPD     bool `yaml:"lgpd"`
	PIPEDA   bool `yaml:"pipeda"`
	APPI     bool `yaml:"appi"`
	PDPA     bool `yaml:"pdpa"`
}

type TrackingConfig struct {
	Type string `yaml:"type"`
	ID   string `yaml:"id"`
	URL  string `yaml:"url"`
}

// ServerSecurityConfig contains cryptographic material required for encrypted
// security-report storage.
type ServerSecurityConfig struct {
	EncryptionKey  string                `yaml:"encryption_key"`
	Reports        SecurityReportsConfig `yaml:"reports"`
	Blocklists     BlocklistsConfig      `yaml:"blocklists"`
	AbuseDetection AbuseDetectionConfig  `yaml:"abuse_detection"`
	Allowlist      []AllowlistEntry      `yaml:"allowlist"`
	BlockedIPs     []BlockedIPEntry      `yaml:"blocked_ips"`
}

type AbuseDetectionConfig struct {
	RequestFlood AbuseRequestFloodConfig `yaml:"request_flood"`
	AutoBlockIP  bool                    `yaml:"auto_block_ip"`
	AutoAlert    bool                    `yaml:"auto_alert"`
}

type AbuseRequestFloodConfig struct {
	Multiplier    int    `yaml:"multiplier"`
	Window        string `yaml:"window"`
	BlockDuration string `yaml:"block_duration"`
}

type AllowlistEntry struct {
	CIDR        string `yaml:"cidr"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type BlockedIPEntry struct {
	CIDR   string `yaml:"cidr"`
	Reason string `yaml:"reason"`
}

type BlocklistsConfig struct {
	Enabled      bool                    `yaml:"enabled"`
	ExtraSources []BlocklistSourceConfig `yaml:"extra_sources"`
}

type BlocklistSourceConfig struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	URL     string `yaml:"url"`
	Enabled *bool  `yaml:"enabled"`
}

type DataConfig struct {
	SecurityDir string          `yaml:"security_dir"`
	CVE         CVEDataConfig   `yaml:"cve"`
	Trivy       TrivyDataConfig `yaml:"trivy"`
}

type CVEDataConfig struct {
	Source      string `yaml:"source"`
	FilterByCPE bool   `yaml:"filter_by_cpe"`
	APIKey      string `yaml:"api_key"`
}

type TrivyDataConfig struct {
	Enabled bool   `yaml:"enabled"`
	Source  string `yaml:"source"`
}

type SecurityReportsConfig struct {
	RetentionDays int `yaml:"retention_days"`
}

// ContactConfig defines public contact destinations and role-specific mailboxes.
type ContactConfig struct {
	Admin    ContactRecipientConfig `yaml:"admin"`
	General  ContactRecipientConfig `yaml:"general"`
	Security ContactRecipientConfig `yaml:"security"`
	Abuse    ContactRecipientConfig `yaml:"abuse"`
	DPO      ContactRecipientConfig `yaml:"dpo"`
}

// ContactRecipientConfig names the configured recipient for a public contact role.
type ContactRecipientConfig struct {
	Name     string            `yaml:"name"`
	Email    string            `yaml:"email"`
	Webhooks map[string]string `yaml:"webhooks,omitempty"`
}

// PagesConfig contains settings for standard public pages.
type PagesConfig struct {
	Contact ContactPageConfig `yaml:"contact"`
	Help    HelpPagesConfig   `yaml:"help"`
}

type HelpPagesConfig struct {
	Content string `yaml:"content"`
	MCP     string `yaml:"mcp"`
}

// ContactPageConfig configures the contact page and its spam challenge.
type ContactPageConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Captcha          string `yaml:"captcha"`
	CaptchaSiteKey   string `yaml:"captcha_site_key"`
	CaptchaSecretKey string `yaml:"captcha_secret_key"`
	SuccessMessage   string `yaml:"success_message"`
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
// release channel (stable|beta|devel); AutoInstall lets the update_check
// scheduler task install a found update rather than only notifying (default
// off — installing is always an explicit operator decision); DeferDays gates
// the scheduled task to releases at least this many days old.
type UpdateConfig struct {
	Branch        string `yaml:"branch"`
	AutoInstall   bool   `yaml:"auto_install"`
	DeferDays     int    `yaml:"defer_days"`
	CLIMinVersion string `yaml:"cli_min_version"`
}

// CLIConfig controls optional client binary distribution.
type CLIConfig struct {
	BinaryDownload CLIBinaryDownloadConfig `yaml:"binary_download"`
}

// CLIBinaryDownloadConfig configures the operator-provisioned CLI release directory.
type CLIBinaryDownloadConfig struct {
	Dir         string `yaml:"dir,omitempty"`
	RequireAuth bool   `yaml:"require_auth,omitempty"`
}

// NotificationsConfig contains the operator/visitor notification settings
// (AI.md PART 17). The public WebUI toast settings are visitor-facing; Email
// carries the SMTP + template configuration used for operator alerts.
type NotificationsConfig struct {
	WebUI         WebUINotifyConfig   `yaml:"webui"`
	Email         EmailConfig         `yaml:"email"`
	DiskLow       DiskLowConfig       `yaml:"disk_low"`
	DatabaseError DatabaseErrorConfig `yaml:"database_error"`
}

type DiskLowConfig struct {
	Enabled          bool    `yaml:"enabled"`
	ThresholdPercent float64 `yaml:"threshold_percent"`
	CheckInterval    string  `yaml:"check_interval"`
	Repeat           string  `yaml:"repeat"`
}

// DatabaseErrorConfig controls consecutive-failure alerts for database probes.
type DatabaseErrorConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Failures      int    `yaml:"failures"`
	CheckInterval string `yaml:"check_interval"`
	Repeat        string `yaml:"repeat"`
}

// WebUINotifyConfig controls the visitor-facing toast notifications rendered by
// the public frontend (AI.md PART 17 → "Public WebUI Notification System").
type WebUINotifyConfig struct {
	Position        string `yaml:"position"`
	Duration        int    `yaml:"duration"`
	WarningDuration int    `yaml:"warning_duration"`
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
	UpdateFailed     bool `yaml:"update_failed"`
	UpdateStaged     bool `yaml:"update_staged"`
	DatabaseError    bool `yaml:"database_error"`
}

// DefaultNotificationsConfig returns the AI.md PART 17 default notification
// settings. Email starts disabled at runtime and is enabled only when a working
// SMTP server is detected or configured.
func DefaultNotificationsConfig() NotificationsConfig {
	return NotificationsConfig{
		WebUI: WebUINotifyConfig{
			Position:        "top-right",
			Duration:        3,
			WarningDuration: 5,
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
				UpdateFailed:     true,
				UpdateStaged:     true,
				DatabaseError:    true,
			},
		},
		DiskLow:       DiskLowConfig{Enabled: true, ThresholdPercent: 85, CheckInterval: "5m", Repeat: "24h"},
		DatabaseError: DatabaseErrorConfig{Enabled: true, Failures: 3, CheckInterval: "1m", Repeat: "24h"},
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
	Expose       bool   `yaml:"expose" json:"expose"`
	ContactEmail string `yaml:"contact_email" json:"contact_email"`
	// Binary is the path to the external tor executable. Empty means
	// auto-detect from PATH and common install locations.
	Binary string `yaml:"binary" json:"binary"`
	// UseNetwork enables an outbound Tor SOCKS dialer in addition to the hidden
	// service. When false the service is hidden-service-only (SocksPort 0).
	UseNetwork bool `yaml:"use_network" json:"use_network"`
	// MaxCircuits caps the number of concurrent circuits.
	MaxCircuits int `yaml:"max_circuits" json:"max_circuits"`
	// StartupTimeout bounds how long startup waits for Tor's control-port file.
	StartupTimeout int `yaml:"startup_timeout" json:"startup_timeout"`
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
		Expose:                    false,
		ContactEmail:              "",
		Binary:                    "",
		UseNetwork:                false,
		MaxCircuits:               32,
		StartupTimeout:            60,
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
	Enabled        bool                `yaml:"enabled"`
	DenyCountries  []string            `yaml:"deny_countries"`
	AllowCountries []string            `yaml:"allow_countries"`
	Presets        map[string][]string `yaml:"presets"`
}

// DefaultGeoIPConfig returns the AI.md PART 19 defaults. GeoIP is enabled,
// country blocking is off, and no operator-authored presets are preloaded.
func DefaultGeoIPConfig() GeoIPConfig {
	return GeoIPConfig{
		Enabled:        true,
		DenyCountries:  []string{},
		AllowCountries: []string{},
		Presets:        map[string][]string{},
	}
}

// I18nConfig contains internationalization settings (AI.md PART 30). The set
// of available languages is compiled into the binary via go:embed; these
// fields select the startup default and the configured server language set.
type I18nConfig struct {
	DefaultLanguage    string   `yaml:"default_language"`
	AvailableLanguages []string `yaml:"available_languages"`
}

// BrandingConfig contains display/branding settings
type BrandingConfig struct {
	Title       string                    `yaml:"title"`
	Tagline     string                    `yaml:"tagline"`
	Description string                    `yaml:"description"`
	Favicon     string                    `yaml:"favicon"`
	Logo        string                    `yaml:"logo"`
	Content     BrandingPageContentConfig `yaml:"content"`
}

type BrandingPageContentConfig struct {
	About   OperatorPageContent `yaml:"about"`
	Privacy OperatorPageContent `yaml:"privacy"`
	Terms   OperatorPageContent `yaml:"terms"`
}

type OperatorPageContent struct {
	Format string      `yaml:"format"`
	Body   interface{} `yaml:"body"`
}

// SEOConfig contains SEO-related settings
type SEOConfig struct {
	Keywords       []string              `yaml:"keywords"`
	Author         string                `yaml:"author"`
	OGImage        string                `yaml:"og_image"`
	TwitterHandle  string                `yaml:"twitter_handle"`
	TitleSeparator string                `yaml:"title_separator"`
	MetaTags       map[string]string     `yaml:"meta_tags"`
	Canonical      string                `yaml:"canonical"`
	NoIndex        bool                  `yaml:"noindex"`
	OpenGraph      OpenGraphSEOConfig    `yaml:"opengraph"`
	Twitter        TwitterSEOConfig      `yaml:"twitter"`
	Verification   VerificationSEOConfig `yaml:"verification"`
	Sitemap        SitemapSEOConfig      `yaml:"sitemap"`
	Feed           FeedSEOConfig         `yaml:"feed"`
}

type OpenGraphSEOConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Type     string `yaml:"type"`
	SiteName string `yaml:"site_name"`
}

type TwitterSEOConfig struct {
	Enabled bool   `yaml:"enabled"`
	Card    string `yaml:"card"`
	Creator string `yaml:"creator"`
}

type VerificationSEOConfig struct {
	Google    string                `yaml:"google"`
	Bing      string                `yaml:"bing"`
	Yandex    string                `yaml:"yandex"`
	Baidu     string                `yaml:"baidu"`
	Pinterest string                `yaml:"pinterest"`
	Facebook  string                `yaml:"facebook"`
	Custom    []VerificationMetaTag `yaml:"custom"`
}

type VerificationMetaTag struct {
	Name    string `yaml:"name"`
	Content string `yaml:"content"`
}

type SitemapSEOConfig struct {
	MaxURLs       int  `yaml:"max_urls"`
	IncludeImages bool `yaml:"include_images"`
}

type FeedSEOConfig struct {
	MaxItems int    `yaml:"max_items"`
	Title    string `yaml:"title"`
}

// AdminConfig contains admin panel settings (NOT credentials — those live in the database)
type AdminConfig struct {
	Email string `yaml:"email"`
}

// SSLConfig contains SSL/TLS settings
type SSLConfig struct {
	Enabled     bool              `yaml:"enabled"`
	LetsEncrypt LetsEncryptConfig `yaml:"letsencrypt"`
}

// LetsEncryptConfig contains Let's Encrypt settings. DNSProvider selects a
// DNS-01 provider (AI.md PART 15); its credentials live in DNSCredentials,
// encrypted at rest.
type LetsEncryptConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Email          string            `yaml:"email"`
	Challenge      string            `yaml:"challenge"`
	DNSProvider    string            `yaml:"dns_provider"`
	DNSCredentials map[string]string `yaml:"dns_credentials"`
}

// ScheduleConfig contains scheduler settings (AI.md PART 18). The scheduler is
// always running; Enabled is retained for backward compatibility but no longer
// gates startup. Per-task enable/disable and run history live in server.db.
type ScheduleConfig struct {
	Timezone      string `yaml:"timezone"`
	CatchUpWindow string `yaml:"catch_up_window"`
}

// RateLimitConfig contains the always-on endpoint and global rate budgets.
type RateLimitConfig struct {
	Read         RateLimitRuleConfig `yaml:"read"`
	Write        RateLimitRuleConfig `yaml:"write"`
	Health       RateLimitRuleConfig `yaml:"health"`
	Strict       RateLimitRuleConfig `yaml:"strict"`
	AuthFailures RateLimitRuleConfig `yaml:"auth_failures"`
	GlobalBurst  int                 `yaml:"global_burst"`
}

type RateLimitRuleConfig struct {
	Requests int `yaml:"requests"`
	Window   int `yaml:"window"`
}

// MetricsConfig controls the internal metrics services (AI.md PART 20).
type MetricsConfig struct {
	Enabled         bool              `yaml:"enabled"`
	Root            MetricsRootConfig `yaml:"root"`
	Auth            MetricsAuthConfig `yaml:"auth"`
	IncludeSystem   bool              `yaml:"include_system"`
	IncludeRuntime  bool              `yaml:"include_runtime"`
	Loki            MetricsLokiConfig `yaml:"loki"`
	DurationBuckets []float64         `yaml:"duration_buckets"`
	SizeBuckets     []float64         `yaml:"size_buckets"`
}

// MetricsRootConfig controls the scraper-compatible root aliases.
type MetricsRootConfig struct {
	Enabled bool `yaml:"enabled"`
}

// MetricsAuthConfig controls per-service bearer authentication.
type MetricsAuthConfig struct {
	AllowUnauthenticated bool               `yaml:"allow_unauthenticated"`
	Tokens               MetricsTokenConfig `yaml:"tokens"`
}

// MetricsTokenConfig stores one independent credential per metrics service.
type MetricsTokenConfig struct {
	Prometheus string `yaml:"prometheus"`
	Grafana    string `yaml:"grafana"`
	Loki       string `yaml:"loki"`
}

// MetricsLokiConfig bounds the amount and age of log data exposed by Loki.
type MetricsLokiConfig struct {
	MaxEntries int    `yaml:"max_entries"`
	MaxAge     string `yaml:"max_age"`
}

// TrustedProxiesConfig lists upstream reverse proxies whose X-Forwarded-*
// headers may be trusted. Private ranges and the listen /24 are always
// trusted; Additional adds public IPs/CIDRs/DNS names (AI.md PART 12).
type TrustedProxiesConfig struct {
	Additional     []string `yaml:"additional"`
	ClientIPHeader string   `yaml:"client_ip_header"`
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	Driver   string              `yaml:"driver"`
	URL      string              `yaml:"url,omitempty"`
	Token    string              `yaml:"token,omitempty"`
	Pool     DatabasePoolConfig  `yaml:"pool"`
	Cache    DatabaseCacheConfig `yaml:"cache"`
	Host     string              `yaml:"host,omitempty"`
	Port     int                 `yaml:"port,omitempty"`
	Name     string              `yaml:"name,omitempty"`
	Username string              `yaml:"username,omitempty"`
	Password string              `yaml:"password,omitempty"`
	SSLMode  string              `yaml:"sslmode,omitempty"`
}

// DatabaseCacheConfig controls the optional local SQLite replica of libSQL.
type DatabaseCacheConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Path         string `yaml:"path,omitempty"`
	SyncInterval string `yaml:"sync_interval"`
	OfflineMode  bool   `yaml:"offline_mode"`
	MaxAge       string `yaml:"max_age"`
}

// DatabasePoolConfig controls open database connections.
type DatabasePoolConfig struct {
	MaxOpen     int    `yaml:"max_open"`
	MaxIdle     int    `yaml:"max_idle"`
	MaxLifetime string `yaml:"max_lifetime"`
	MaxIdleTime string `yaml:"max_idle_time"`
}

// CacheConfig describes the optional in-process or remote Valkey/Redis cache.
type CacheConfig struct {
	Type          string `yaml:"type"`
	URL           string `yaml:"url,omitempty"`
	Host          string `yaml:"host,omitempty"`
	Port          int    `yaml:"port,omitempty"`
	Username      string `yaml:"username,omitempty"`
	Password      string `yaml:"password,omitempty"`
	DB            int    `yaml:"db,omitempty"`
	TLS           bool   `yaml:"tls"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
	TLSCA         string `yaml:"tls_ca,omitempty"`
	TLSCert       string `yaml:"tls_cert,omitempty"`
	TLSKey        string `yaml:"tls_key,omitempty"`
	PoolSize      int    `yaml:"pool_size"`
	MinIdle       int    `yaml:"min_idle"`
	Timeout       string `yaml:"timeout"`
	Prefix        string `yaml:"prefix"`
	TTL           string `yaml:"ttl"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level    string            `yaml:"level"`
	Defaults LogDefaultsConfig `yaml:"defaults"`
	Access   LogFileConfig     `yaml:"access"`
	Server   LogFileConfig     `yaml:"server"`
	Error    LogFileConfig     `yaml:"error"`
	Audit    AuditLogConfig    `yaml:"audit"`
	Security LogFileConfig     `yaml:"security"`
	Debug    LogFileConfig     `yaml:"debug"`
}

type LogDefaultsConfig struct {
	Custom string `yaml:"custom"`
	Rotate string `yaml:"rotate"`
	Keep   string `yaml:"keep"`
}

type LogFileConfig struct {
	Enabled  *bool  `yaml:"enabled,omitempty"`
	Format   string `yaml:"format"`
	Filename string `yaml:"filename,omitempty"`
	Custom   string `yaml:"custom,omitempty"`
	Rotate   string `yaml:"rotate,omitempty"`
	Keep     string `yaml:"keep,omitempty"`
	Compress *bool  `yaml:"compress,omitempty"`
}

type AuditLogConfig struct {
	LogFileConfig    `yaml:",inline"`
	Events           AuditLogEvents `yaml:"events"`
	IncludeUserAgent bool           `yaml:"include_user_agent"`
}

type AuditLogEvents struct {
	Configuration bool `yaml:"configuration"`
	Security      bool `yaml:"security"`
	Backup        bool `yaml:"backup"`
	Server        bool `yaml:"server"`
}

func boolPointer(value bool) *bool {
	return &value
}

// MaintenanceConfig contains maintenance mode settings
type MaintenanceConfig struct {
	SelfHealing SelfHealingConfig `yaml:"self_healing"`
	Cleanup     CleanupConfig     `yaml:"cleanup"`
	Notify      NotifyConfig      `yaml:"notify"`
}

// SelfHealingConfig contains self-healing settings
type SelfHealingConfig struct {
	RetryInterval RetryInterval `yaml:"retry_interval"`
	MaxAttempts   int           `yaml:"max_attempts"`
}

// RetryInterval stores retry pacing as whole seconds while accepting YAML
// durations such as 30s and legacy integer seconds.
type RetryInterval int

func (interval *RetryInterval) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind != yaml.ScalarNode {
		return fmt.Errorf("retry interval must be a scalar duration")
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(node.Value)); err == nil {
		if seconds <= 0 {
			return fmt.Errorf("retry interval must be positive")
		}
		*interval = RetryInterval(seconds)
		return nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(node.Value))
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return fmt.Errorf("retry interval must be a positive whole-second duration")
	}
	*interval = RetryInterval(duration / time.Second)
	return nil
}

// CleanupConfig contains auto-cleanup thresholds
type CleanupConfig struct {
	DiskThreshold    int `yaml:"disk_threshold"`
	LogRetentionDays int `yaml:"log_retention_days"`
}

// NotifyConfig contains notification settings for maintenance events
type NotifyConfig struct {
	OnEnter bool `yaml:"on_enter"`
	OnExit  bool `yaml:"on_exit"`
}

// BackupConfig contains backup settings (AI.md PART 21).
type BackupConfig struct {
	Encryption         BackupEncryptionConfig `yaml:"encryption"`
	EncryptionPassword string                 `yaml:"encryption_password"`
	Retention          BackupRetentionConfig  `yaml:"retention"`
}

type RestoreConfig struct {
	MaxArchiveSize  string `yaml:"max_archive_size"`
	MaxExpandedSize string `yaml:"max_expanded_size"`
	MaxFiles        int    `yaml:"max_files"`
	MaxFileSize     string `yaml:"max_file_size"`
	MaxRatio        int    `yaml:"max_ratio"`
}

// BackupRetentionConfig controls historical full and incremental backups.
type BackupRetentionConfig struct {
	MaxBackups   int    `yaml:"max_backups"`
	KeepWeekly   int    `yaml:"keep_weekly"`
	KeepMonthly  int    `yaml:"keep_monthly"`
	KeepYearly   int    `yaml:"keep_yearly"`
	MaxTotalSize string `yaml:"max_total_size"`
}

// BackupEncryptionConfig controls whether backup encryption is configured.
type BackupEncryptionConfig struct {
	Enabled bool `yaml:"enabled"`
}

// WebConfig contains frontend/web settings
type WebConfig struct {
	UI            WebUIConfig            `yaml:"ui"`
	Robots        WebRobotsConfig        `yaml:"robots"`
	Reports       WebReportsConfig       `yaml:"reports"`
	HSTS          WebHSTSConfig          `yaml:"hsts"`
	CSP           WebCSPConfig           `yaml:"csp"`
	Headers       WebHeadersConfig       `yaml:"headers"`
	Announcements WebAnnouncementsConfig `yaml:"announcements"`
	CORS          string                 `yaml:"cors"`
	Security      WebSecurityConfig      `yaml:"security"`
	APIContact    string                 `yaml:"api_contact"`
	LLMS          LLMSConfig             `yaml:"llms"`
}

type WebHSTSConfig struct {
	Enabled           bool  `yaml:"enabled"`
	MaxAgeSeconds     int64 `yaml:"max_age_seconds"`
	IncludeSubdomains bool  `yaml:"include_subdomains"`
	Preload           bool  `yaml:"preload"`
}

type WebCSPConfig struct {
	Enabled                bool    `yaml:"enabled"`
	Mode                   string  `yaml:"mode"`
	DefaultSrcExtra        string  `yaml:"default_src_extra"`
	ScriptSrcExtra         string  `yaml:"script_src_extra"`
	StyleSrcExtra          string  `yaml:"style_src_extra"`
	ImgSrcExtra            string  `yaml:"img_src_extra"`
	FontSrcExtra           string  `yaml:"font_src_extra"`
	ConnectSrcExtra        string  `yaml:"connect_src_extra"`
	MediaSrcExtra          string  `yaml:"media_src_extra"`
	WorkerSrcExtra         string  `yaml:"worker_src_extra"`
	ManifestSrcExtra       string  `yaml:"manifest_src_extra"`
	FrameSrcExtra          string  `yaml:"frame_src_extra"`
	FrameAncestorsExtra    string  `yaml:"frame_ancestors_extra"`
	BaseURIExtra           string  `yaml:"base_uri_extra"`
	FormActionExtra        string  `yaml:"form_action_extra"`
	ObjectSrcExtra         string  `yaml:"object_src_extra"`
	DefaultSrcOverride     string  `yaml:"default_src_override"`
	ScriptSrcOverride      string  `yaml:"script_src_override"`
	StyleSrcOverride       string  `yaml:"style_src_override"`
	ImgSrcOverride         string  `yaml:"img_src_override"`
	FontSrcOverride        string  `yaml:"font_src_override"`
	ConnectSrcOverride     string  `yaml:"connect_src_override"`
	MediaSrcOverride       string  `yaml:"media_src_override"`
	WorkerSrcOverride      string  `yaml:"worker_src_override"`
	ManifestSrcOverride    string  `yaml:"manifest_src_override"`
	FrameSrcOverride       string  `yaml:"frame_src_override"`
	FrameAncestorsOverride string  `yaml:"frame_ancestors_override"`
	BaseURIOverride        string  `yaml:"base_uri_override"`
	FormActionOverride     string  `yaml:"form_action_override"`
	ObjectSrcOverride      string  `yaml:"object_src_override"`
	ReportsEnabled         bool    `yaml:"reports_enabled"`
	ReportsSampleRate      float64 `yaml:"reports_sample_rate"`
}

type WebHeadersConfig struct {
	COOP                    string                 `yaml:"coop"`
	COEP                    string                 `yaml:"coep"`
	CORP                    string                 `yaml:"corp"`
	ReferrerPolicy          string                 `yaml:"referrer_policy"`
	OriginAgentCluster      bool                   `yaml:"origin_agent_cluster"`
	CrossDomainPolicies     string                 `yaml:"cross_domain_policies"`
	DNSPrefetchControl      string                 `yaml:"dns_prefetch_control"`
	HonorSecGPC             bool                   `yaml:"honor_sec_gpc"`
	HonorDNT                bool                   `yaml:"honor_dnt"`
	SecFetchValidation      bool                   `yaml:"sec_fetch_validation"`
	ServerTimingInDebugOnly bool                   `yaml:"server_timing_in_debug_only"`
	ClearSiteData           WebClearSiteDataConfig `yaml:"clear_site_data"`
	NEL                     WebNELConfig           `yaml:"nel"`
}

type WebClearSiteDataConfig struct {
	OnTokenRevocation   bool `yaml:"on_token_revocation"`
	OnConsentWithdrawal bool `yaml:"on_consent_withdrawal"`
	ExecutionContexts   bool `yaml:"execution_contexts"`
}

type WebNELConfig struct {
	Enabled           bool    `yaml:"enabled"`
	MaxAgeSeconds     int64   `yaml:"max_age_seconds"`
	IncludeSubdomains bool    `yaml:"include_subdomains"`
	SampleRate        float64 `yaml:"sample_rate"`
}

type WebAnnouncementsConfig struct {
	Enabled  bool                    `yaml:"enabled"`
	Messages []WebAnnouncementConfig `yaml:"messages"`
}

type WebAnnouncementConfig struct {
	ID          string `yaml:"id"`
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Message     string `yaml:"message"`
	Start       string `yaml:"start"`
	End         string `yaml:"end"`
	Dismissible bool   `yaml:"dismissible"`
}

// WebReportsConfig limits public browser reporting endpoints.
type WebReportsConfig struct {
	RateLimitPerMinute  int `yaml:"rate_limit_per_minute"`
	RateLimitPerIPBurst int `yaml:"rate_limit_per_ip_burst"`
}

type LLMSConfig struct {
	IncludeEndpoints bool     `yaml:"include_endpoints"`
	IncludeSchemas   bool     `yaml:"include_schemas"`
	CustomSections   []string `yaml:"custom_sections"`
}

// WebUIConfig contains web UI settings
type WebUIConfig struct {
	Theme  string `yaml:"theme"`
	Accent string `yaml:"accent"`
}

// WebRobotsConfig contains robots.txt allow/deny path settings
type WebRobotsConfig struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

// WebSecurityConfig contains security-related web settings
type WebSecurityConfig struct {
	Admin         string   `yaml:"admin"`
	CORS          string   `yaml:"cors"`
	ReportURL     string   `yaml:"report_url"`
	Contact       string   `yaml:"contact"`
	Expires       string   `yaml:"expires"`
	Policy        string   `yaml:"policy"`
	PublishPGPKey bool     `yaml:"publish_pgp_key"`
	Keyservers    []string `yaml:"keyservers"`
}

type CORSConfig struct {
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

var (
	current              *Config
	mu                   sync.RWMutex
	configPath           string
	legacyBackupPending  = map[string]bool{}
	lastGoodConfigYAML   = map[string][]byte{}
	invalidConfigFiles   = map[string]bool{}
	configValueLocations = map[string]configValueLocation{}
)

type configValueLocation struct {
	file string
	line int
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:       "",
			FQDN:       "",
			Address:    "[::]",
			BaseURL:    "/",
			APIVersion: "v1",
			Daemonize:  false,
			Mode:       "production",
			Debug: DebugConfig{
				Pprof: true, LogQueries: true, LogCache: true,
				MaxBodyLogSize: "10KB", BlockProfileRate: 1,
				MutexProfileFraction: 1, RuntimeEndpoints: true,
			},
			PIDFile: true,
			Update: UpdateConfig{
				Branch:      "stable",
				AutoInstall: false,
				DeferDays:   0,
			},
			Branding: BrandingConfig{
				Title:       "gitignore",
				Tagline:     "",
				Description: "",
				Favicon:     "",
				Logo:        "",
			},
			Privacy: DefaultPrivacyConfig(),
			SEO: SEOConfig{
				Keywords: []string{}, TitleSeparator: " - ", MetaTags: map[string]string{},
				OpenGraph:    OpenGraphSEOConfig{Enabled: true, Type: "website"},
				Twitter:      TwitterSEOConfig{Enabled: true, Card: "summary_large_image"},
				Verification: VerificationSEOConfig{Custom: []VerificationMetaTag{}},
				Sitemap:      SitemapSEOConfig{MaxURLs: 50000}, Feed: FeedSEOConfig{MaxItems: 50},
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
				Timezone:      "America/New_York",
				CatchUpWindow: "1h",
			},
			RateLimit: RateLimitConfig{
				Read:         RateLimitRuleConfig{Requests: 120, Window: 60},
				Write:        RateLimitRuleConfig{Requests: 10, Window: 60},
				Health:       RateLimitRuleConfig{Requests: 120, Window: 60},
				Strict:       RateLimitRuleConfig{Requests: 10, Window: 900},
				AuthFailures: RateLimitRuleConfig{Requests: 5, Window: 900},
				GlobalBurst:  240,
			},
			Metrics: MetricsConfig{
				Enabled:         true,
				Root:            MetricsRootConfig{Enabled: true},
				Auth:            MetricsAuthConfig{},
				IncludeSystem:   true,
				IncludeRuntime:  true,
				Loki:            MetricsLokiConfig{MaxEntries: 1000, MaxAge: "1h"},
				DurationBuckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
				SizeBuckets:     []float64{100, 1000, 10000, 100000, 1000000, 10000000},
			},
			TrustedProxies: TrustedProxiesConfig{
				Additional:     []string{},
				ClientIPHeader: "auto",
			},
			CORS: CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true, MaxAge: 86400},
			Database: DatabaseConfig{
				Driver: "sqlite",
				Pool:   DatabasePoolConfig{MaxOpen: 25, MaxIdle: 5, MaxLifetime: "5m", MaxIdleTime: "1m"},
				Cache:  DatabaseCacheConfig{Enabled: false, SyncInterval: "30s", OfflineMode: true, MaxAge: "1h"},
			},
			Cache: CacheConfig{
				Type: "memory", Host: "localhost", Port: 6379, TLSSkipVerify: true,
				PoolSize: 10, MinIdle: 2, Timeout: "5s", Prefix: "gitignore:", TTL: "1h",
			},
			Logging: LoggingConfig{
				Level:    "",
				Defaults: LogDefaultsConfig{Rotate: "weekly,50MB", Keep: "none"},
				Access:   LogFileConfig{Format: "apache", Rotate: "monthly"},
				Server:   LogFileConfig{Format: "text"},
				Error:    LogFileConfig{Format: "text"},
				Audit: AuditLogConfig{
					LogFileConfig:    LogFileConfig{Enabled: boolPointer(true), Format: "json", Rotate: "daily", Compress: boolPointer(false)},
					Events:           AuditLogEvents{Configuration: true, Security: true, Backup: true, Server: true},
					IncludeUserAgent: true,
				},
				Security: LogFileConfig{Format: "fail2ban"},
				Debug:    LogFileConfig{Enabled: boolPointer(false), Format: "text"},
			},
			Maintenance: MaintenanceConfig{
				SelfHealing: SelfHealingConfig{
					RetryInterval: 30,
					MaxAttempts:   0,
				},
				Cleanup: CleanupConfig{
					DiskThreshold:    90,
					LogRetentionDays: 7,
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
				EncryptionPassword: "",
				Retention:          BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "10%"},
			},
			Restore:    RestoreConfig{MaxArchiveSize: "1G", MaxExpandedSize: "4G", MaxFiles: 10000, MaxFileSize: "512M", MaxRatio: 100},
			Compliance: ComplianceConfig{Enabled: false},
			I18n: I18nConfig{
				DefaultLanguage:    "en",
				AvailableLanguages: []string{"en", "es", "fr", "ar", "de", "it", "zh", "ja"},
			},
			Tor:           DefaultTorConfig(),
			I2P:           DefaultI2PConfig(),
			Tokens:        OwnerTokensConfig{MaxLinked: 25, CookieMaxAge: "1y", IdlePurgeDays: 0},
			GeoIP:         DefaultGeoIPConfig(),
			Notifications: DefaultNotificationsConfig(),
			Security: ServerSecurityConfig{
				Reports:    SecurityReportsConfig{RetentionDays: 365},
				Blocklists: BlocklistsConfig{Enabled: true, ExtraSources: []BlocklistSourceConfig{}},
				AbuseDetection: AbuseDetectionConfig{
					RequestFlood: AbuseRequestFloodConfig{Multiplier: 10, Window: "60s", BlockDuration: "1h"},
					AutoBlockIP:  true, AutoAlert: true,
				},
			},
			Pages: PagesConfig{
				Contact: ContactPageConfig{
					Enabled:        true,
					Captcha:        "simple",
					SuccessMessage: "Your message was sent successfully.",
				},
				Help: HelpPagesConfig{
					Content: "Use the catalog API to list, search, retrieve, combine, and export curated .gitignore templates. Template names are case-insensitive. See the API reference for route and response details.",
					MCP:     "Use the public catalog tools to list, search, retrieve, combine, and export curated .gitignore templates. Template names are case-insensitive.",
				},
			},
		},
		Web: WebConfig{
			HSTS: WebHSTSConfig{Enabled: true, MaxAgeSeconds: 63072000, IncludeSubdomains: true, Preload: true},
			CSP:  WebCSPConfig{Enabled: true, Mode: "enforce", ReportsEnabled: true, ReportsSampleRate: 1},
			Headers: WebHeadersConfig{
				COOP: "unsafe-none", COEP: "unsafe-none", CORP: "cross-origin",
				ReferrerPolicy:     "strict-origin-when-cross-origin",
				OriginAgentCluster: true, CrossDomainPolicies: "none", HonorSecGPC: true,
				SecFetchValidation: true, ServerTimingInDebugOnly: true,
				ClearSiteData: WebClearSiteDataConfig{OnTokenRevocation: true, OnConsentWithdrawal: true},
				NEL:           WebNELConfig{Enabled: true, MaxAgeSeconds: 2592000, IncludeSubdomains: true, SampleRate: 1},
			},
			Announcements: WebAnnouncementsConfig{Enabled: true, Messages: []WebAnnouncementConfig{}},
			Robots:        WebRobotsConfig{Allow: []string{"/", "/api"}, Deny: []string{"/debug"}},
			UI: WebUIConfig{
				Theme: "dark", Accent: "",
			},
			Reports: WebReportsConfig{
				RateLimitPerMinute:  60,
				RateLimitPerIPBurst: 10,
			},
			CORS: "*",
			LLMS: LLMSConfig{IncludeEndpoints: true},
			Security: WebSecurityConfig{
				Admin:         "",
				CORS:          "*",
				PublishPGPKey: true,
				Keyservers:    []string{},
			},
		},
		Data: DataConfig{
			SecurityDir: "{data_dir}/security",
			CVE:         CVEDataConfig{Source: "https://services.nvd.nist.gov/rest/json/cves/2.0", FilterByCPE: false},
			Trivy:       TrivyDataConfig{Enabled: false, Source: "ghcr.io/aquasecurity/trivy-db:2"},
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
				fmt.Println(configText("cli.config.file_migrated", "old_path", oldPath, "new_path", path))
			}
		}
	}
}

// Load loads configuration from a YAML file
func Load(path string) (*Config, error) {
	mu.Lock()
	defer mu.Unlock()

	migrateYamlToYml(path)
	if err := migrateLegacyServerConfig(path); err != nil {
		fmt.Fprintln(os.Stderr, configText("cli.config.legacy_migration_warning"))
	}
	if err := migrateAreaConfig(path); err != nil {
		fmt.Fprintln(os.Stderr, configText("cli.config.area_migration_warning"))
	}
	configPath = path
	configValueLocations = make(map[string]configValueLocation)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := loadYAMLDropins(path, cfg); err != nil {
			return nil, err
		}
		seedFirstRunConnections(cfg)
		if err := loadSecretsConfig(path, cfg); err != nil {
			return nil, err
		}
		validateBackupConfig(cfg)
		validateRestoreConfig(cfg)
		validateDebugConfig(cfg)
		validateLoggingConfig(cfg)
		validateNotificationsConfig(cfg)
		validateAbuseDetectionConfig(cfg)
		validateTorConfig(cfg)
		warnIgnoredConnectionEnvironment(cfg)
		applyConnectionURLTypes(cfg)
		if err := applyI2PEnvironment(cfg); err != nil {
			return nil, err
		}
		if _, err := loadSensitiveSecrets(cfg, path); err != nil {
			return nil, err
		}
		if cfg.Server.Port == "" {
			cfg.Server.Port = defaultServerPort()
		} else if normalized, ok := NormalizePort(cfg.Server.Port); ok {
			cfg.Server.Port = normalized
		} else {
			fmt.Fprintln(os.Stderr, "invalid server port; selecting a valid default")
			cfg.Server.Port = defaultServerPort()
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
			return nil, fmt.Errorf("%s: %w", configText("cli.config.default_create_failed"), err)
		}
		if err := migrateAreaConfig(path); err != nil {
			fmt.Fprintln(os.Stderr, configText("cli.config.area_migration_warning"))
		}
		expandConfigEnvironment(cfg)
		current = cfg
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", configText("cli.config.file_read_failed"), err)
	}

	cfg := DefaultConfig()
	cfg.LegacySecrets = extractLegacySecretsFromConfig(path)
	mainConfigInvalid := false
	if err := applyYAMLConfigFile(path, data, cfg); err != nil {
		mainConfigInvalid = true
		fmt.Fprintln(os.Stderr, configText("cli.config.file_parse_warning", "file", filepath.Base(path)))
		if lastGood, ok := lastGoodConfigYAML[configFileCacheKey(path)]; ok {
			if applyErr := applyYAMLConfigFile(path, lastGood, cfg); applyErr != nil {
				cfg = DefaultConfig()
			}
		}
	}
	if err := loadYAMLDropins(path, cfg); err != nil {
		return nil, err
	}
	if err := loadSecretsConfig(path, cfg); err != nil {
		return nil, err
	}
	validateBackupConfig(cfg)
	validateRestoreConfig(cfg)
	validateDebugConfig(cfg)
	validateLoggingConfig(cfg)
	validateNotificationsConfig(cfg)
	validateAbuseDetectionConfig(cfg)
	validateTorConfig(cfg)
	warnIgnoredConnectionEnvironment(cfg)
	applyConnectionURLTypes(cfg)
	if err := applyI2PEnvironment(cfg); err != nil {
		return nil, err
	}
	legacySecurityKey := strings.TrimSpace(cfg.Server.Security.EncryptionKey) != ""
	needsSecurityKey, err := loadSensitiveSecrets(cfg, path)
	if err != nil {
		return nil, err
	}
	if err := validateContactCaptcha(cfg); err != nil {
		return nil, err
	}
	if err := validateClientIPHeader(cfg); err != nil {
		return nil, err
	}
	if err := validateSecurityDataConfig(cfg); err != nil {
		return nil, err
	}
	preserveLegacySecretSource := len(cfg.LegacySecrets) > 0
	if (needsSecurityKey || legacySecurityKey) && !mainConfigInvalid && !preserveLegacySecretSource {
		if err := saveConfig(cfg, path); err != nil {
			return nil, fmt.Errorf("%s: %w", configText("cli.config.migrated_security_save_failed"), err)
		}
	}
	branch, ok := NormalizeUpdateBranch(cfg.Server.Update.Branch)
	if !ok {
		warnInvalidConfigKey("server.update.branch")
		branch = DefaultConfig().Server.Update.Branch
		cfg.Server.Update.Branch = branch
	}
	if branch != cfg.Server.Update.Branch && !mainConfigInvalid && !preserveLegacySecretSource {
		cfg.Server.Update.Branch = branch
		if err := saveConfig(cfg, path); err != nil {
			return nil, fmt.Errorf("%s: %w", configText("cli.config.update_branch_normalize_failed"), err)
		}
		if err := writeConfigArea(path, "maintenance", cfg); err != nil {
			return nil, fmt.Errorf("%s: %w", configText("cli.config.update_branch_save_failed"), err)
		}
	}

	// Fill in computed defaults for existing configs missing these fields
	if cfg.Server.Port == "" {
		cfg.Server.Port = defaultServerPort()
		if !mainConfigInvalid && !preserveLegacySecretSource {
			_ = saveConfig(cfg, path)
		}
	} else if normalized, ok := NormalizePort(cfg.Server.Port); ok {
		cfg.Server.Port = normalized
	} else {
		fmt.Fprintln(os.Stderr, "invalid server port; selecting a valid default")
		cfg.Server.Port = defaultServerPort()
		if !mainConfigInvalid && !preserveLegacySecretSource {
			_ = saveConfig(cfg, path)
		}
	}
	if cfg.Server.FQDN == "" {
		cfg.Server.FQDN = detectFQDN()
	}

	expandConfigEnvironment(cfg)
	current = cfg
	return cfg, nil
}

func extractLegacySecrets(data []byte) map[string]string {
	var document yaml.Node
	if yaml.Unmarshal(data, &document) != nil || len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	legacy := map[string]string{
		"installation_secret":                  "installation_secret",
		"installation_secret_previous":         "installation_secret_previous",
		"installation_secret_previous_expires": "installation_secret_previous_until",
		"encryption_key_previous":              "encryption_key_previous",
		"encryption_key_previous_expires":      "encryption_key_previous_until",
	}
	values := make(map[string]string)
	for oldName, canonicalName := range legacy {
		node := mappingConfigPath(root, []string{"server", "security", oldName})
		if node != nil && node.Kind == yaml.ScalarNode && strings.TrimSpace(node.Value) != "" {
			values[canonicalName] = node.Value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func extractLegacySecretsFromConfig(path string) map[string]string {
	secrets := make(map[string]string)
	paths := []string{path, filepath.Join(filepath.Dir(path), "yaml.d", "security.yml")}
	for _, source := range paths {
		data, err := os.ReadFile(source)
		if err != nil {
			continue
		}
		for name, value := range extractLegacySecrets(data) {
			secrets[name] = value
		}
	}
	if len(secrets) == 0 {
		return nil
	}
	return secrets
}

// CompleteLegacySecretMigration removes imported legacy secret keys only after
// the database transaction has durably imported them.
func CompleteLegacySecretMigration(path string) error {
	sources := []string{path, filepath.Join(filepath.Dir(path), "yaml.d", "security.yml")}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			return err
		}
		if len(document.Content) == 0 {
			continue
		}
		root := document.Content[0]
		changed := false
		for _, key := range []string{"installation_secret", "installation_secret_previous", "installation_secret_previous_expires", "encryption_key_previous", "encryption_key_previous_expires"} {
			changed = removeConfigPath(root, []string{"server", "security", key}) != nil || changed
		}
		if !changed {
			continue
		}
		if source != path || !legacyBackupPending[path] {
			if err := copyConfigBackup(source, data); err != nil {
				return err
			}
		}
		encoded, err := yaml.Marshal(&document)
		if err != nil {
			return err
		}
		if source == path {
			const header = "# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n"
			if !strings.HasPrefix(string(encoded), "# This node only:") {
				encoded = append([]byte(header), encoded...)
			}
		}
		if err := writeConfigAtomically(source, encoded); err != nil {
			return err
		}
	}
	return nil
}

// StartupSettings contains the four non-secret values needed before normal
// configuration loading during startup.
type StartupSettings struct {
	Port      string
	Address   string
	Daemonize bool
	PIDFile   bool
}

// PeekStartupSettings reads only server.port, server.address,
// server.daemonize, and server.pidfile from server.yml and ordered drop-ins.
// It is read-only and ignores files that cannot be parsed; Load reports those
// errors during the full configuration pass.
func PeekStartupSettings(path string) StartupSettings {
	defaults := DefaultConfig().Server
	settings := StartupSettings{Port: defaults.Port, Address: defaults.Address, Daemonize: defaults.Daemonize, PIDFile: defaults.PIDFile}
	paths := []string{path}
	dir := filepath.Join(filepath.Dir(path), "yaml.d")
	if entries, err := os.ReadDir(dir); err == nil {
		var dropins []string
		for _, entry := range entries {
			if entry.IsDir() || ignoredDropinName(entry.Name()) || !isYAMLDropin(entry.Name()) {
				continue
			}
			filePath := filepath.Join(dir, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				resolved, err := filepath.EvalSymlinks(filePath)
				if err != nil || !pathWithinDirectory(dir, resolved) {
					continue
				}
				filePath = resolved
			}
			info, err := os.Stat(filePath)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			dropins = append(dropins, filePath)
		}
		sort.Strings(dropins)
		paths = append(paths, dropins...)
	}
	for _, filePath := range paths {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var document yaml.Node
		if yaml.Unmarshal(data, &document) != nil || len(document.Content) == 0 {
			continue
		}
		root := document.Content[0]
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value != "server" || root.Content[i+1].Kind != yaml.MappingNode {
				continue
			}
			server := root.Content[i+1]
			for j := 0; j+1 < len(server.Content); j += 2 {
				key, value := server.Content[j].Value, server.Content[j+1]
				occurrences := 0
				for k := 0; k+1 < len(server.Content); k += 2 {
					if server.Content[k].Value == key {
						occurrences++
					}
				}
				if occurrences != 1 || value.Kind != yaml.ScalarNode || value.Tag == "!!null" {
					resetStartupSetting(&settings, defaults, key)
					continue
				}
				switch key {
				case "port":
					var parsed string
					if value.Decode(&parsed) == nil {
						settings.Port = parsed
					} else {
						settings.Port = defaults.Port
					}
				case "address":
					var parsed string
					if value.Decode(&parsed) == nil {
						settings.Address = parsed
					} else {
						settings.Address = defaults.Address
					}
				case "daemonize":
					var parsed bool
					if value.Decode(&parsed) == nil {
						settings.Daemonize = parsed
					} else {
						settings.Daemonize = defaults.Daemonize
					}
				case "pidfile":
					var parsed bool
					if value.Decode(&parsed) == nil {
						settings.PIDFile = parsed
					} else {
						settings.PIDFile = defaults.PIDFile
					}
				}
			}
		}
	}
	return settings
}

func resetStartupSetting(settings *StartupSettings, defaults ServerConfig, key string) {
	switch key {
	case "port":
		settings.Port = defaults.Port
	case "address":
		settings.Address = defaults.Address
	case "daemonize":
		settings.Daemonize = defaults.Daemonize
	case "pidfile":
		settings.PIDFile = defaults.PIDFile
	}
}

func migrateLegacyServerConfig(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil
	}
	if len(document.Content) == 0 {
		return nil
	}
	server := mappingValue(document.Content[0], "server")
	if server == nil || server.Kind != yaml.MappingNode {
		return nil
	}
	removedIdentity := removeConfigMapping(server, "user") != nil
	removedIdentity = removeConfigMapping(server, "group") != nil || removedIdentity
	if legacySchedule := mappingValue(server, "schedule"); legacySchedule != nil && legacySchedule.Kind == yaml.MappingNode {
		legacyTasks := map[string]bool{
			"ssl_renewal": true, "geoip_update": true, "blocklist_update": true, "cve_update": true,
			"update_check": true, "token_cleanup": true, "log_rotation": true, "backup_daily": true,
			"backup_hourly": true, "disk_check": true, "database_check": true, "healthcheck_self": true,
			"health_registry_refresh": true, "ip_block_cleanup": true, "security_cleanup": true,
			"keyserver_retry": true, "tor_health": true, "i2p_health": true,
		}
		for index := len(legacySchedule.Content) - 2; index >= 0; index -= 2 {
			key := legacySchedule.Content[index].Value
			if legacyTasks[key] {
				legacySchedule.Content = append(legacySchedule.Content[:index], legacySchedule.Content[index+2:]...)
			}
		}
	}
	changed := removedIdentity || moveConfigMapping(server, "schedule", "scheduler")
	changed = moveConfigMapping(server, "logging", "logs") || changed
	changed = migrateLegacyKeys(document.Content[0]) || changed
	if scheduler := mappingValue(server, "scheduler"); scheduler != nil && scheduler.Kind == yaml.MappingNode {
		if removeConfigMapping(scheduler, "enabled") != nil {
			changed = true
		}
	}
	logs := mappingValue(server, "logs")
	if logs != nil && logs.Kind == yaml.MappingNode {
		if accessFormat := removeConfigMapping(logs, "access_format"); accessFormat != nil {
			access := mappingValue(logs, "access")
			if access == nil {
				access = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				logs.Content = append(logs.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "access"}, access)
			}
			if access.Kind == yaml.MappingNode && mappingValue(access, "format") == nil {
				access.Content = append(access.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "format"}, accessFormat)
			}
			changed = true
		}
	}
	if !changed {
		delete(legacyBackupPending, path)
		return nil
	}
	if !legacyBackupPending[path] {
		if err := copyConfigBackup(path, data); err != nil {
			return err
		}
	}
	legacyBackupPending[path] = true
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	const header = "# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n"
	if !strings.HasPrefix(string(encoded), "# This node only:") {
		encoded = append([]byte(header), encoded...)
	}
	return writeConfigAtomically(path, encoded)
}

func migrateLegacyKeys(root *yaml.Node) bool {
	changed := false
	changed = removeConfigPath(root, []string{"server", "web", "cookie_consent", "enabled"}) != nil || changed
	changed = migrateLegacyContent(root, []string{"web", "footer", "custom_html"}, []string{"server", "branding", "content", "footer"}, "html", false) || changed
	changed = migrateLegacyContent(root, []string{"server", "branding", "footer_text"}, []string{"server", "branding", "content", "footer"}, "html", true) || changed
	for _, page := range []string{"about", "privacy", "terms"} {
		changed = migrateLegacyContent(root, []string{"server", "pages", page, "content"}, []string{"server", "branding", "content", page}, "md", false) || changed
	}
	if database := mappingValue(root, "database"); database != nil && database.Kind == yaml.MappingNode {
		if primary := mappingValue(database, "primary"); primary != nil && primary.Kind == yaml.MappingNode {
			mergeConfigNodeMissing(database, primary)
			removeConfigMapping(database, "primary")
			changed = true
		}
		changed = moveConfigMapping(database, "type", "driver") || changed
		changed = moveLegacyConfigPath(root, []string{"database"}, []string{"server", "database"}) || changed
	}
	mappings := []struct{ old, new []string }{
		{[]string{"server", "web", "cookie_consent", "message"}, []string{"server", "privacy", "consent", "message"}},
		{[]string{"server", "web", "cookie_consent", "policy_url"}, []string{"server", "privacy", "consent", "policy", "url"}},
		{[]string{"server", "security_headers", "hsts"}, []string{"web", "hsts", "enabled"}},
		{[]string{"server", "security_headers", "hsts_max_age"}, []string{"web", "hsts", "max_age_seconds"}},
		{[]string{"server", "security", "hsts"}, []string{"web", "hsts"}},
		{[]string{"server", "security", "nel"}, []string{"web", "headers", "nel"}},
		{[]string{"server", "security", "headers", "clear_site_data"}, []string{"web", "headers", "clear_site_data"}},
		{[]string{"server", "security", "csrf"}, []string{"server", "csrf"}},
		{[]string{"web", "csrf"}, []string{"server", "csrf"}},
		{[]string{"server", "title"}, []string{"server", "branding", "title"}},
		{[]string{"server", "description"}, []string{"server", "branding", "description"}},
		{[]string{"server", "seo", "default_title"}, []string{"server", "branding", "title"}},
		{[]string{"server", "seo", "default_description"}, []string{"server", "branding", "description"}},
		{[]string{"server", "seo", "opengraph", "image"}, []string{"server", "seo", "og_image"}},
		{[]string{"server", "seo", "twitter", "site"}, []string{"server", "seo", "twitter_handle"}},
		{[]string{"server", "branding", "logo_url"}, []string{"server", "branding", "logo"}},
		{[]string{"server", "branding", "favicon_url"}, []string{"server", "branding", "favicon"}},
		{[]string{"server", "branding", "primary_color"}, []string{"web", "ui", "accent"}},
		{[]string{"web", "favicon_url"}, []string{"server", "branding", "favicon"}},
		{[]string{"web", "ui", "favicon"}, []string{"server", "branding", "favicon"}},
		{[]string{"web", "ui", "logo"}, []string{"server", "branding", "logo"}},
		{[]string{"server", "admin", "email"}, []string{"server", "contact", "admin", "email"}},
		{[]string{"server", "ssl", "dns01", "provider"}, []string{"server", "ssl", "letsencrypt", "dns_provider"}},
		{[]string{"server", "ssl", "dns01", "credentials_encrypted"}, []string{"server", "ssl", "letsencrypt", "dns_credentials"}},
		{[]string{"server", "ssl", "letsencrypt", "provider"}, []string{"server", "ssl", "letsencrypt", "dns_provider"}},
		{[]string{"server", "ssl", "letsencrypt", "credentials_encrypted"}, []string{"server", "ssl", "letsencrypt", "dns_credentials"}},
		{[]string{"server", "web"}, []string{"web"}},
	}
	for _, mapping := range mappings {
		changed = moveLegacyConfigPath(root, mapping.old, mapping.new) || changed
	}
	for _, mapping := range mappings {
		if mapping.old[0] == "web" {
			changed = moveLegacyConfigPath(root, mapping.old, mapping.new) || changed
		}
	}
	changed = migrateLegacyContent(root, []string{"web", "footer", "custom_html"}, []string{"server", "branding", "content", "footer"}, "html", false) || changed
	changed = normalizeLegacySchedulerTasks(root) || changed
	if httpsPort := removeConfigPath(root, []string{"server", "https_port"}); httpsPort != nil {
		changed = true
		portNode := mappingConfigPath(root, []string{"server", "port"})
		if portNode != nil && portNode.Kind == yaml.ScalarNode && !strings.Contains(portNode.Value, ",") {
			port, portErr := strconv.Atoi(strings.TrimSpace(portNode.Value))
			secure, secureErr := strconv.Atoi(strings.TrimSpace(httpsPort.Value))
			if portErr == nil && port > 0 && port <= 65535 && secureErr == nil && secure > 0 && secure <= 65535 {
				portNode.Value = fmt.Sprintf("%d,%d", port, secure)
				portNode.Tag = "!!str"
				portNode.Style = yaml.DoubleQuotedStyle
			}
		}
	}
	for _, key := range [][]string{
		{"server", "web", "cookie_consent", "enabled"},
		{"server", "security_headers"},
		{"server", "security", "headers"},
		{"server", "seo", "enabled"}, {"server", "seo", "sitemap", "enabled"}, {"server", "seo", "feed", "enabled"},
		{"server", "seo", "opengraph", "description"}, {"server", "branding", "theme_color"}, {"server", "branding", "source_code_url"},
		{"server", "ssl", "dns01", "validated_at"}, {"server", "ssl", "dns01", "domain"}, {"server", "ssl", "dns01", "domains"},
		{"server", "ssl", "letsencrypt", "validated_at"}, {"server", "ssl", "letsencrypt", "domain"}, {"server", "ssl", "letsencrypt", "domains"},
		{"server", "admin", "path"}, {"server", "admin", "password"}, {"server", "admin", "token"}, {"server", "admin", "username"}, {"server", "admin", "two_factor"},
		{"web", "llms", "enabled"}, {"server", "privacy", "content"}, {"server", "mcp"},
		{"server", "rate_limit", "enabled"}, {"server", "rate_limit", "requests"}, {"server", "rate_limit", "window"},
		{"server", "scheduler", "enabled"}, {"server", "maintenance_mode"}, {"server", "i18n", "auto_detect"}, {"server", "i18n", "rtl_languages"}, {"server", "i18n", "show_selector"}, {"server", "i18n", "translations_dir"},
		{"server", "geoip", "dir"}, {"server", "geoip", "databases"}, {"server", "geoip", "country_mode"}, {"server", "database", "sqlite"},
		{"server", "metrics", "endpoint"}, {"server", "notifications", "email_enabled"}, {"server", "notifications", "webhook_enabled"},
		{"server", "secret_key"}, {"web", "permissions_policy"}, {"web", "honor_dnt"}, {"web", "robots", "ai_bots"},
	} {
		changed = removeConfigPath(root, key) != nil || changed
	}
	return changed
}

func normalizeLegacySchedulerTasks(root *yaml.Node) bool {
	tasks := mappingConfigPath(root, []string{"server", "scheduler", "tasks"})
	if tasks == nil || tasks.Kind != yaml.MappingNode {
		return false
	}
	allowed := map[string]bool{"enabled": true, "schedule": true, "retry_on_fail": true, "retry_delay": true}
	defaults := map[string]map[string][]string{
		"geoip_update":     {"enabled": {"true"}, "schedule": {"0 3 * * 0"}, "retry_on_fail": {"true"}, "retry_delay": {"1h"}},
		"blocklist_update": {"enabled": {"true"}, "schedule": {"0 4 * * *"}, "retry_on_fail": {"true"}, "retry_delay": {"1h"}},
		"cve_update":       {"enabled": {"true"}, "schedule": {"0 5 * * *"}, "retry_on_fail": {"true"}, "retry_delay": {"1h"}},
		"log_rotation":     {"enabled": {"true"}, "schedule": {"0 0 * * *"}},
		"backup_daily":     {"enabled": {"true"}, "schedule": {"0 2 * * *"}},
		"backup_hourly":    {"enabled": {"false"}, "schedule": {"@hourly"}},
		"ssl_renewal":      {"enabled": {"true"}, "schedule": {"0 3 * * *"}},
		"token_cleanup":    {"enabled": {"true"}, "schedule": {"*/15 * * * *", "@every 15m"}},
		"healthcheck_self": {"enabled": {"true"}, "schedule": {"*/5 * * * *", "@every 5m"}},
		"tor_health":       {"enabled": {"true"}, "schedule": {"*/10 * * * *", "@every 10m"}},
	}
	changed := false
	for taskIndex := len(tasks.Content) - 2; taskIndex >= 0; taskIndex -= 2 {
		taskName := tasks.Content[taskIndex].Value
		task := tasks.Content[taskIndex+1]
		if task.Kind != yaml.MappingNode {
			continue
		}
		for keyIndex := len(task.Content) - 2; keyIndex >= 0; keyIndex -= 2 {
			if !allowed[task.Content[keyIndex].Value] {
				task.Content = append(task.Content[:keyIndex], task.Content[keyIndex+2:]...)
				changed = true
			}
		}
		if len(task.Content) == 0 {
			tasks.Content = append(tasks.Content[:taskIndex], tasks.Content[taskIndex+2:]...)
			changed = true
			continue
		}
		defaultsForTask := defaults[taskName]
		if len(defaultsForTask) == 0 {
			continue
		}
		matchesDefault := true
		for keyIndex := 0; keyIndex+1 < len(task.Content); keyIndex += 2 {
			key, value := task.Content[keyIndex].Value, task.Content[keyIndex+1]
			validValues := defaultsForTask[key]
			if value.Kind != yaml.ScalarNode || !containsDefaultValue(validValues, value.Value) {
				matchesDefault = false
				break
			}
		}
		if matchesDefault {
			tasks.Content = append(tasks.Content[:taskIndex], tasks.Content[taskIndex+2:]...)
			changed = true
		}
	}
	if len(tasks.Content) == 0 {
		changed = removeConfigPath(root, []string{"server", "scheduler", "tasks"}) != nil || changed
	}
	return changed
}

func containsDefaultValue(values []string, expected string) bool {
	for _, value := range values {
		if expected == value {
			return true
		}
	}
	return false
}

func migrateLegacyContent(root *yaml.Node, oldPath, newPath []string, format string, wrapParagraph bool) bool {
	value := removeConfigPath(root, oldPath)
	if value == nil {
		return false
	}
	if mappingConfigPath(root, newPath) != nil {
		return true
	}
	body := *value
	if wrapParagraph && body.Kind == yaml.ScalarNode {
		body.Value = "<p>" + html.EscapeString(body.Value) + "</p>"
	}
	content := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "format"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: format},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "body"}, &body,
	}}
	parent := root
	for _, key := range newPath[:len(newPath)-1] {
		parent = ensureConfigMapping(parent, key)
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: newPath[len(newPath)-1]}, content)
	return true
}

func moveLegacyConfigPath(root *yaml.Node, oldPath, newPath []string) bool {
	value := mappingConfigPath(root, oldPath)
	if value == nil {
		return false
	}
	value = removeConfigPath(root, oldPath)
	if existing := mappingConfigPath(root, newPath); existing != nil {
		seoEmptyTarget := len(oldPath) == 4 && oldPath[0] == "server" && oldPath[1] == "seo" && ((oldPath[2] == "opengraph" && oldPath[3] == "image") || (oldPath[2] == "twitter" && oldPath[3] == "site")) ||
			len(oldPath) == 3 && oldPath[0] == "server" && oldPath[1] == "seo" && (oldPath[2] == "default_title" || oldPath[2] == "default_description")
		if seoEmptyTarget && existing.Kind == yaml.ScalarNode && existing.Value == "" {
			parent := root
			for _, key := range newPath[:len(newPath)-1] {
				parent = mappingValue(parent, key)
			}
			for index := 0; index+1 < len(parent.Content); index += 2 {
				if parent.Content[index].Value == newPath[len(newPath)-1] {
					parent.Content[index+1] = value
					return true
				}
			}
		}
		if existing.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			mergeConfigNodeMissing(existing, value)
		}
		return true
	}
	parent := root
	for _, key := range newPath[:len(newPath)-1] {
		parent = ensureConfigMapping(parent, key)
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: newPath[len(newPath)-1]}, value)
	return true
}

var nodeConfigKeys = map[string]bool{
	"port": true, "address": true, "fqdn": true, "mode": true, "api_version": true,
	"baseurl": true, "pidfile": true, "daemonize": true, "debug": true,
	"database": true, "cache": true, "ssl": true, "trusted_proxies": true, "cluster": true,
}

var serverConfigAreas = map[string]string{
	"cors": "web", "csrf": "web", "url_detection": "web", "compression": "web", "healthz": "web", "i18n": "web",
	"branding": "branding", "seo": "branding", "pages": "pages", "contact": "contact",
	"privacy": "privacy", "compliance": "privacy", "tracking": "privacy",
	"security": "security", "rate_limit": "security", "geoip": "security", "limits": "security", "tokens": "security",
	"logs": "logs", "metrics": "metrics", "notifications": "notifications",
	"tor": "tor", "i2p": "i2p", "maintenance": "maintenance", "scheduler": "maintenance",
	"update": "maintenance", "auto_update": "maintenance", "backup": "maintenance", "restore": "maintenance",
}

var areaConfigFiles = map[string]string{
	"branding": "branding.yml", "web": "web.yml", "pages": "pages.yml", "contact": "contact.yml",
	"privacy": "privacy.yml", "security": "security.yml", "logs": "logs.yml", "metrics": "metrics.yml",
	"notifications": "notifications.yml", "tor": "tor.yml", "i2p": "i2p.yml", "maintenance": "maintenance.yml", "app": "app.yml",
}

func migrateAreaConfig(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := document.Content[0]
	areaDir := filepath.Join(filepath.Dir(path), "yaml.d")
	if err := migrateLegacyMaintenanceDropin(path, areaDir, data); err != nil {
		return err
	}
	changed := false
	secrets := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	secretsPath := filepath.Join(filepath.Dir(path), "secrets.yml")
	if secretData, err := os.ReadFile(secretsPath); err == nil {
		var secretDocument yaml.Node
		if yaml.Unmarshal(secretData, &secretDocument) != nil || len(secretDocument.Content) == 0 || secretDocument.Content[0].Kind != yaml.MappingNode {
			return fmt.Errorf("%s", configText("cli.config.existing_secrets_invalid"))
		}
		secrets = secretDocument.Content[0]
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	secretValues := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	secretsChanged := false
	for _, secretPath := range [][]string{
		{"server", "token"},
		{"server", "database", "token"},
		{"server", "cache", "password"},
		{"server", "security", "encryption_key"},
		{"server", "security", "encryption_key_version"},
		{"server", "metrics", "auth", "tokens", "prometheus"},
		{"server", "metrics", "auth", "tokens", "grafana"},
		{"server", "metrics", "auth", "tokens", "loki"},
		{"server", "notifications", "email", "smtp", "password"},
		{"server", "pages", "contact", "captcha_secret_key"},
		{"server", "ssl", "letsencrypt", "dns_credentials"},
		{"server", "backup", "encryption_password"},
		{"data", "cve", "api_key"},
	} {
		if value := removeConfigPath(root, secretPath); value != nil {
			if value.Value != "" || value.Kind == yaml.MappingNode {
				setConfigPath(secretValues, secretPath, value)
				secretsChanged = true
			}
			changed = true
		}
	}
	for _, role := range []string{"admin", "security", "abuse", "general"} {
		secretPath := []string{"server", "contact", role, "webhooks"}
		if value := removeConfigPath(root, secretPath); value != nil {
			if value.Kind == yaml.MappingNode && len(value.Content) > 0 {
				setConfigPath(secretValues, secretPath, value)
				secretsChanged = true
			}
			changed = true
		}
	}
	for _, secretPath := range [][]string{{"server", "database", "url"}, {"server", "cache", "url"}} {
		if value := mappingConfigPath(root, secretPath); value != nil && urlContainsCredentials(value.Value) {
			removeConfigPath(root, secretPath)
			setConfigPath(secretValues, secretPath, value)
			secretsChanged = true
			changed = true
		}
	}
	if secretsChanged {
		mergeSecretNode(secrets, secretValues)
	}
	areas := make(map[string]*yaml.Node)
	changed = changed || secretsChanged
	getArea := func(name string) *yaml.Node {
		if areas[name] == nil {
			areas[name] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		return areas[name]
	}
	server := mappingValue(root, "server")
	if server != nil && server.Kind == yaml.MappingNode {
		for index := len(server.Content) - 2; index >= 0; index -= 2 {
			key, value := server.Content[index], server.Content[index+1]
			if nodeConfigKeys[key.Value] {
				continue
			}
			area := serverConfigAreas[key.Value]
			if area == "" {
				area = "app"
			}
			mergeMissingConfigNode(ensureConfigMapping(getArea(area), "server"), key.Value, value)
			server.Content = append(server.Content[:index], server.Content[index+2:]...)
			changed = true
		}
		if len(server.Content) == 0 {
			removeConfigMapping(root, "server")
		}
	}
	for _, section := range []string{"web", "data", "tor"} {
		mapping := mappingValue(root, section)
		if mapping == nil || mapping.Kind != yaml.MappingNode {
			continue
		}
		area := "web"
		switch section {
		case "data":
			area = "security"
		case "tor":
			area = "tor"
		}
		for index := len(mapping.Content) - 2; index >= 0; index -= 2 {
			key, value := mapping.Content[index], mapping.Content[index+1]
			keyArea := area
			if section == "web" && key.Value == "ui" {
				keyArea = "branding"
			}
			mergeMissingConfigNode(ensureConfigMapping(getArea(keyArea), section), key.Value, value)
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			changed = true
		}
		removeConfigMapping(root, section)
	}
	for index := len(root.Content) - 2; index >= 0; index -= 2 {
		key, value := root.Content[index], root.Content[index+1]
		if key.Value == "server" || key.Value == "web" || key.Value == "data" || key.Value == "tor" || key.Value == "secrets" {
			continue
		}
		mergeMissingConfigNode(getArea("app"), key.Value, value)
		root.Content = append(root.Content[:index], root.Content[index+2:]...)
		changed = true
	}
	if !changed {
		delete(legacyBackupPending, path)
		return nil
	}
	configDir := filepath.Dir(path)
	areaDir = filepath.Join(configDir, "yaml.d")
	if err := os.MkdirAll(areaDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(areaDir, 0700); err != nil {
		return err
	}
	if !legacyBackupPending[path] {
		if err := copyConfigBackup(path, data); err != nil {
			return err
		}
	}
	delete(legacyBackupPending, path)
	if secretsChanged {
		encoded, err := yaml.Marshal(secrets)
		if err != nil {
			return err
		}
		if err := writeConfigAtomically(secretsPath, encoded); err != nil {
			return err
		}
	}
	existingDropins := readExistingDropinConfig(areaDir)
	for name, area := range areas {
		filename := areaConfigFiles[name]
		if filename == "" || len(area.Content) == 0 {
			continue
		}
		mergeExistingAreaConfig(area, existingDropins, name)
		if name == "maintenance" {
			normalizeLegacySchedulerTasks(area)
		}
		if emptyAreaConfig(area) {
			continue
		}
		encoded, err := yaml.Marshal(area)
		if err != nil {
			return err
		}
		areaPath := filepath.Join(areaDir, filename)
		if err := writeConfigAtomically(areaPath, append([]byte("# Generated from server.yml (AI.md PART 5).\n"), encoded...)); err != nil {
			return err
		}
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	const header = "# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n"
	return writeConfigAtomically(path, append([]byte(header), encoded...))
}

func migrateLegacyMaintenanceDropin(serverPath, areaDir string, serverData []byte) error {
	path := filepath.Join(areaDir, "maintenance.yml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 {
		return err
	}
	if !normalizeLegacySchedulerTasks(document.Content[0]) {
		return nil
	}
	if !legacyBackupPending[serverPath] {
		if err := copyConfigBackup(serverPath, serverData); err != nil {
			return err
		}
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	return writeConfigAtomically(path, append([]byte("# Generated from server.yml (AI.md PART 5).\n"), encoded...))
}

func readExistingDropinConfig(directory string) *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return root
	}
	for _, entry := range entries {
		if entry.IsDir() || ignoredDropinName(entry.Name()) || !isYAMLDropin(entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || !pathWithinDirectory(directory, resolved) {
				continue
			}
			info, err = os.Stat(resolved)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			path = resolved
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var document yaml.Node
		if yaml.Unmarshal(data, &document) != nil || len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
			continue
		}
		mergeConfigNodeOverride(root, document.Content[0])
	}
	return root
}

func mergeExistingAreaConfig(area, existing *yaml.Node, name string) {
	if area == nil || existing == nil {
		return
	}
	filtered := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for index := 0; index+1 < len(area.Content); index += 2 {
		rootKey := area.Content[index].Value
		section := mappingValue(existing, rootKey)
		if section == nil || section.Kind != yaml.MappingNode {
			continue
		}
		allowed := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if rootKey == "server" {
			for keyIndex := 0; keyIndex+1 < len(section.Content); keyIndex += 2 {
				key := section.Content[keyIndex].Value
				areaName := serverConfigAreas[key]
				if areaName == "" && !nodeConfigKeys[key] {
					areaName = "app"
				}
				if areaName == name {
					allowed.Content = append(allowed.Content, section.Content[keyIndex], section.Content[keyIndex+1])
				}
			}
		} else if rootKey == "web" {
			for keyIndex := 0; keyIndex+1 < len(section.Content); keyIndex += 2 {
				key := section.Content[keyIndex].Value
				if name == "branding" && key == "ui" || name == "web" && key != "ui" {
					allowed.Content = append(allowed.Content, section.Content[keyIndex], section.Content[keyIndex+1])
				}
			}
		} else if rootKey == "data" && name == "security" || rootKey == "tor" && name == "tor" {
			allowed = section
		}
		if len(allowed.Content) == 0 {
			continue
		}
		filtered.Content = append(filtered.Content, area.Content[index], allowed)
	}
	mergeConfigNodeOverride(area, filtered)
}

func mergeConfigNodeOverride(target, source *yaml.Node) {
	if target.Kind != yaml.MappingNode || source.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(source.Content); index += 2 {
		key, value := source.Content[index], source.Content[index+1]
		existing := mappingValue(target, key.Value)
		if existing == nil {
			target.Content = append(target.Content, key, value)
			continue
		}
		if existing.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			mergeConfigNodeOverride(existing, value)
			continue
		}
		for childIndex := 1; childIndex < len(target.Content); childIndex += 2 {
			if target.Content[childIndex-1].Value == key.Value {
				target.Content[childIndex] = value
				break
			}
		}
	}
}

func emptyAreaConfig(area *yaml.Node) bool {
	for index := 1; index < len(area.Content); index += 2 {
		if area.Content[index].Kind != yaml.MappingNode || len(area.Content[index].Content) > 0 {
			return false
		}
	}
	return true
}

func mappingConfigPath(root *yaml.Node, path []string) *yaml.Node {
	current := root
	for _, key := range path {
		current = mappingValue(current, key)
		if current == nil {
			return nil
		}
	}
	return current
}

func urlContainsCredentials(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.User != nil {
		return true
	}
	query := parsed.Query()
	for _, key := range []string{"password", "pwd", "token", "authToken"} {
		if query.Get(key) != "" {
			return true
		}
	}
	return false
}

func removeConfigPath(root *yaml.Node, path []string) *yaml.Node {
	if len(path) == 0 || root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	parent := root
	for _, key := range path[:len(path)-1] {
		parent = mappingValue(parent, key)
		if parent == nil || parent.Kind != yaml.MappingNode {
			return nil
		}
	}
	value := removeConfigMapping(parent, path[len(path)-1])
	for index := len(path) - 1; index > 0; index-- {
		if len(parent.Content) != 0 {
			break
		}
		pathKey := path[index-1]
		grandParent := root
		for _, key := range path[:index-1] {
			grandParent = mappingValue(grandParent, key)
			if grandParent == nil || grandParent.Kind != yaml.MappingNode {
				return value
			}
		}
		removeConfigMapping(grandParent, pathKey)
		parent = grandParent
	}
	return value
}

func setConfigPath(root *yaml.Node, path []string, value *yaml.Node) {
	parent := root
	for _, key := range path[:len(path)-1] {
		parent = ensureConfigMapping(parent, key)
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[len(path)-1]}, value)
}

func mergeSecretNode(target, source *yaml.Node) {
	if target.Kind != yaml.MappingNode || source.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(source.Content); index += 2 {
		key, value := source.Content[index], source.Content[index+1]
		existing := mappingValue(target, key.Value)
		if existing == nil {
			target.Content = append(target.Content, key, value)
			continue
		}
		if existing.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			mergeSecretNode(existing, value)
		} else if existing.Value == "" && value.Value != "" {
			for childIndex := 1; childIndex < len(target.Content); childIndex += 2 {
				if target.Content[childIndex-1].Value == key.Value {
					target.Content[childIndex] = value
					break
				}
			}
		}
	}
}

func ensureConfigMapping(parent *yaml.Node, key string) *yaml.Node {
	if mapping := mappingValue(parent, key); mapping != nil && mapping.Kind == yaml.MappingNode {
		return mapping
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, mapping)
	return mapping
}

func mergeMissingConfigNode(parent *yaml.Node, key string, value *yaml.Node) {
	if existing := mappingValue(parent, key); existing != nil {
		mergeConfigNodeMissing(existing, value)
		return
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func mergeConfigNodeMissing(target, source *yaml.Node) {
	if target.Kind != yaml.MappingNode || source.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(source.Content); index += 2 {
		mergeMissingConfigNode(target, source.Content[index].Value, source.Content[index+1])
	}
}

func moveConfigMapping(mapping *yaml.Node, oldKey, newKey string) bool {
	oldValue := mappingValue(mapping, oldKey)
	if oldValue == nil {
		return false
	}
	newValue := mappingValue(mapping, newKey)
	if newValue == nil {
		for index := 0; index+1 < len(mapping.Content); index += 2 {
			if mapping.Content[index].Value == oldKey {
				mapping.Content[index].Value = newKey
				return true
			}
		}
		return false
	}
	mergeConfigMapping(newValue, oldValue)
	removeConfigMapping(mapping, oldKey)
	return true
}

func mergeConfigMapping(target, source *yaml.Node) {
	if target.Kind != yaml.MappingNode || source.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(source.Content); index += 2 {
		key, value := source.Content[index], source.Content[index+1]
		if existing := mappingValue(target, key.Value); existing != nil {
			mergeConfigMapping(existing, value)
			continue
		}
		target.Content = append(target.Content, key, value)
	}
}

func removeConfigMapping(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			value := mapping.Content[index+1]
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return value
		}
	}
	return nil
}

func copyConfigBackup(path string, data []byte) error {
	backup := path + ".bak"
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return err
	}
	return os.Chmod(backup, 0600)
}

func writeConfigAtomically(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".server.yml-migrate-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

// loadYAMLDropins applies yaml.d files in lexical order over the main file.
// The I2P drop-in remains handled separately because its save path is managed
// by the I2P settings workflow.
func loadYAMLDropins(configPath string, cfg *Config) error {
	dir := filepath.Join(filepath.Dir(configPath), "yaml.d")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, configText("cli.config.dropin_directory_read_warning"))
		return nil
	}
	if err := os.Chmod(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, configText("cli.config.dropin_directory_permissions_warning"))
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if ignoredDropinName(name) || entry.IsDir() {
			continue
		}
		if !isYAMLDropin(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || !pathWithinDirectory(dir, resolved) {
				continue
			}
			info, err = os.Stat(resolved)
			if err != nil {
				continue
			}
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Chmod(path, 0600); err != nil {
			if info.Mode().Perm()&0022 != 0 {
				fmt.Fprintln(os.Stderr, configText("cli.config.dropin_unsafe_warning", "file", name))
				continue
			}
			fmt.Fprintln(os.Stderr, configText("cli.config.dropin_permissions_warning", "file", name))
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, configText("cli.config.dropin_read_warning", "file", filepath.Base(path)))
			data, _ = lastGoodConfigYAML[configFileCacheKey(path)]
			if len(data) == 0 {
				continue
			}
		}
		if err := applyYAMLConfigFile(path, data, cfg); err != nil {
			fmt.Fprintln(os.Stderr, configText("cli.config.dropin_parse_warning", "file", filepath.Base(path)))
			lastGood, ok := lastGoodConfigYAML[configFileCacheKey(path)]
			if !ok || applyYAMLConfigFile(path, lastGood, cfg) != nil {
				continue
			}
		}
	}
	return nil
}

func ignoredDropinName(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".bak") || strings.HasSuffix(lower, "~") || strings.HasSuffix(lower, ".orig")
}

func pathWithinDirectory(directory, path string) bool {
	base, err := filepath.Abs(directory)
	if err != nil {
		return false
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(base, resolved)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func applyYAMLConfigFile(path string, data []byte, current *Config) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s", configText("cli.config.document_mapping_required"))
	}
	root := document.Content[0]
	migrateLegacyRateLimitConfig(path, root)
	web := mappingValue(root, "web")
	if web == nil {
		web = ensureConfigMapping(root, "web")
	}
	if web.Kind == yaml.MappingNode {
		for legacyKey, canonicalKey := range map[string]string{"web_robots": "robots", "web_security": "security"} {
			if legacy := removeConfigMapping(root, legacyKey); legacy != nil {
				if nested := mappingValue(web, canonicalKey); nested == nil {
					web.Content = append(web.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: canonicalKey}, legacy)
				} else {
					mergeConfigNodeMissing(nested, legacy)
				}
			}
		}
	}
	defaults := reflect.ValueOf(DefaultConfig()).Elem()
	target := reflect.ValueOf(current).Elem()
	applyConfigMapping(path, root, target, defaults, "")
	lastGoodConfigYAML[configFileCacheKey(path)] = append([]byte(nil), data...)
	return nil
}

func migrateLegacyRateLimitConfig(path string, root *yaml.Node) {
	server := mappingValue(root, "server")
	limits := mappingValue(server, "rate_limit")
	if limits == nil || limits.Kind != yaml.MappingNode {
		return
	}
	if enabled := removeConfigMapping(limits, "enabled"); enabled != nil {
		fmt.Fprintln(os.Stderr, configText("cli.config.unknown_key_error", "file", filepath.Base(path), "line", enabled.Line, "key", "server.rate_limit.enabled"))
	}
	requests := removeConfigMapping(limits, "requests")
	window := removeConfigMapping(limits, "window")
	if requests == nil && window == nil {
		return
	}
	for _, ruleName := range []string{"read", "write", "health"} {
		rule := mappingValue(limits, ruleName)
		if rule == nil {
			rule = ensureConfigMapping(limits, ruleName)
		}
		if rule.Kind != yaml.MappingNode {
			continue
		}
		if requests != nil && mappingValue(rule, "requests") == nil {
			value := *requests
			rule.Content = append(rule.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "requests"}, &value)
		}
		if window != nil && mappingValue(rule, "window") == nil {
			value := *window
			rule.Content = append(rule.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "window"}, &value)
		}
	}
}

func applyConfigMapping(path string, node *yaml.Node, target, defaults reflect.Value, prefix string) {
	if target.Kind() != reflect.Struct || defaults.Kind() != reflect.Struct || node.Kind != yaml.MappingNode {
		return
	}
	seen := make(map[string]bool, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode, valueNode := node.Content[index], node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			continue
		}
		key := keyNode.Value
		if filepath.Base(path) == "secrets.yml" && ((prefix == "server" && key == "token") || (prefix == "server.security" && key == "encryption_key_version")) {
			continue
		}
		fieldIndex := configFieldIndex(target.Type(), key)
		if fieldIndex < 0 {
			fieldPath := key
			if prefix != "" {
				fieldPath = prefix + "." + key
			}
			fmt.Fprintln(os.Stderr, configText("cli.config.unknown_key_error", "file", filepath.Base(path), "line", valueNode.Line, "key", fieldPath))
			continue
		}
		field := target.Field(fieldIndex)
		defaultField := defaults.Field(fieldIndex)
		for field.Type().Kind() == reflect.Struct && target.Type().Field(fieldIndex).Tag.Get("yaml") == ",inline" {
			nestedIndex := configFieldIndex(field.Type(), key)
			if nestedIndex < 0 {
				break
			}
			field = field.Field(nestedIndex)
			defaultField = defaultField.Field(nestedIndex)
			if field.Kind() != reflect.Struct || field.Type().Field(0).Tag.Get("yaml") != ",inline" {
				break
			}
		}
		fieldPath := key
		if prefix != "" {
			fieldPath = prefix + "." + key
		}
		if target.Type() == reflect.TypeOf(ServerConfig{}) && key == "schedule" {
			fieldPath = strings.TrimSuffix(fieldPath, "schedule") + "scheduler"
		}
		configValueLocations[fieldPath] = configValueLocation{file: filepath.Base(path), line: valueNode.Line}
		if seen[key] {
			field.Set(defaultField)
			warnInvalidConfigValue(path, valueNode.Line, fieldPath)
			continue
		}
		seen[key] = true
		if valueNode.Tag == "!!null" {
			field.Set(defaultField)
			warnInvalidConfigValue(path, valueNode.Line, fieldPath)
			continue
		}
		if field.Kind() == reflect.Struct {
			if field.Type() == reflect.TypeOf(ScheduleConfig{}) && key == "schedule" {
				fieldPath = strings.TrimSuffix(fieldPath, "schedule") + "scheduler"
			}
			if field.Kind() == reflect.Struct && valueNode.Kind == yaml.MappingNode {
				applyConfigMapping(path, valueNode, field, defaultField, fieldPath)
				continue
			}
			field.Set(defaultField)
			warnInvalidConfigValue(path, valueNode.Line, fieldPath)
			continue
		}
		if valueNode.Kind == yaml.MappingNode && field.Kind() == reflect.Map {
			decoded := reflect.New(field.Type())
			if err := valueNode.Decode(decoded.Interface()); err != nil || decoded.Elem().Kind() != reflect.Map {
				field.Set(defaultField)
				warnInvalidConfigValue(path, valueNode.Line, fieldPath)
				continue
			}
			if field.IsNil() {
				field.Set(reflect.MakeMap(field.Type()))
			}
			iterator := decoded.Elem().MapRange()
			for iterator.Next() {
				field.SetMapIndex(iterator.Key(), iterator.Value())
			}
			continue
		}
		if err := valueNode.Decode(field.Addr().Interface()); err != nil {
			field.Set(defaultField)
			warnInvalidConfigValue(path, valueNode.Line, fieldPath)
		}
	}
}

func configFieldIndex(configType reflect.Type, key string) int {
	for index := 0; index < configType.NumField(); index++ {
		field := configType.Field(index)
		tagParts := strings.Split(field.Tag.Get("yaml"), ",")
		tag := tagParts[0]
		if tag == "-" {
			continue
		}
		if containsString(tagParts[1:], "inline") {
			if nested := configFieldIndex(field.Type, key); nested >= 0 {
				return index
			}
			continue
		}
		if tag == "" {
			tag = strings.ToLower(field.Name)
		}
		if tag == key || (configType == reflect.TypeOf(ServerConfig{}) && key == "schedule" && tag == "scheduler") {
			return index
		}
	}
	return -1
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func warnInvalidConfigValue(path string, line int, key string) {
	fmt.Fprintln(os.Stderr, configText("cli.config.invalid_value_warning", "file", filepath.Base(path), "line", line, "key", key))
}

func warnInvalidConfigKey(key string) {
	if location, ok := configValueLocations[key]; ok {
		warnInvalidConfigValue(location.file, location.line, key)
		return
	}
	fmt.Fprintln(os.Stderr, configText("cli.config.invalid_key_warning", "key", key))
}

// IsConfigValueSet reports whether a value was explicitly loaded from config.
func IsConfigValueSet(key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := configValueLocations[key]
	return ok
}

func expandConfigEnvironment(cfg *Config) {
	if cfg == nil {
		return
	}
	var expand func(reflect.Value)
	expand = func(value reflect.Value) {
		switch value.Kind() {
		case reflect.Pointer:
			if !value.IsNil() {
				expand(value.Elem())
			}
		case reflect.Struct:
			for index := 0; index < value.NumField(); index++ {
				field := value.Field(index)
				if field.CanSet() {
					expand(field)
				}
			}
		case reflect.String:
			if value.CanSet() {
				value.SetString(expandEnvironmentValue(value.String()))
			}
		case reflect.Slice:
			for index := 0; index < value.Len(); index++ {
				expand(value.Index(index))
			}
		case reflect.Map:
			if value.IsNil() || value.Type().Key().Kind() != reflect.String || value.Type().Elem().Kind() != reflect.String {
				return
			}
			iterator := value.MapRange()
			for iterator.Next() {
				value.SetMapIndex(iterator.Key(), reflect.ValueOf(expandEnvironmentValue(iterator.Value().String())).Convert(value.Type().Elem()))
			}
		}
	}
	expand(reflect.ValueOf(cfg))
}

func expandEnvironmentValue(value string) string {
	for {
		start := strings.Index(value, "${")
		if start < 0 {
			return value
		}
		relativeEnd := strings.IndexByte(value[start+2:], '}')
		if relativeEnd < 0 {
			return value
		}
		end := start + 2 + relativeEnd
		expression := value[start+2 : end]
		name, fallback, hasFallback := strings.Cut(expression, ":-")
		replacement, found := os.LookupEnv(name)
		if !found || (hasFallback && replacement == "") {
			replacement = ""
			if hasFallback {
				replacement = fallback
			}
		}
		value = value[:start] + replacement + value[end+1:]
	}
}

func configFileCacheKey(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}

func validateSecurityDataConfig(cfg *Config) error {
	seen := map[string]bool{"firehol_level1": true}
	validSources := true
	for _, source := range cfg.Server.Security.Blocklists.ExtraSources {
		id := strings.TrimSpace(source.ID)
		if id == "" {
			validSources = false
			break
		}
		for _, char := range id {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
				validSources = false
				break
			}
		}
		if !validSources {
			break
		}
		if seen[id] {
			validSources = false
			break
		}
		seen[id] = true
		if source.Type != "ip" && source.Type != "domain" {
			validSources = false
			break
		}
		parsed, err := url.Parse(source.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			validSources = false
			break
		}
	}
	if !validSources {
		cfg.Server.Security.Blocklists.ExtraSources = DefaultConfig().Server.Security.Blocklists.ExtraSources
		warnInvalidConfigKey("server.security.blocklists.extra_sources")
	}
	nvd, err := url.Parse(cfg.Data.CVE.Source)
	if err != nil || nvd.Scheme != "https" || nvd.Host == "" || nvd.User != nil {
		cfg.Data.CVE.Source = DefaultConfig().Data.CVE.Source
		warnInvalidConfigKey("data.cve.source")
	}
	if strings.TrimSpace(cfg.Data.Trivy.Source) == "" || strings.Contains(cfg.Data.Trivy.Source, "://") {
		cfg.Data.Trivy.Source = DefaultConfig().Data.Trivy.Source
		warnInvalidConfigKey("data.trivy.source")
	}
	if cfg.Server.Notifications.DiskLow.ThresholdPercent < 1 || cfg.Server.Notifications.DiskLow.ThresholdPercent > 100 {
		cfg.Server.Notifications.DiskLow.ThresholdPercent = DefaultConfig().Server.Notifications.DiskLow.ThresholdPercent
		warnInvalidConfigKey("server.notifications.disk_low.threshold_percent")
	}
	for key, value := range map[string]string{"check_interval": cfg.Server.Notifications.DiskLow.CheckInterval, "repeat": cfg.Server.Notifications.DiskLow.Repeat} {
		if _, err := time.ParseDuration(value); err != nil {
			defaults := DefaultConfig().Server.Notifications.DiskLow
			if key == "check_interval" {
				cfg.Server.Notifications.DiskLow.CheckInterval = defaults.CheckInterval
			} else {
				cfg.Server.Notifications.DiskLow.Repeat = defaults.Repeat
			}
			warnInvalidConfigKey("server.notifications.disk_low." + key)
		}
	}
	return nil
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
		return fmt.Errorf("%s", configText("cli.config.not_loaded"))
	}
	if err := saveConfig(current, configPath); err != nil {
		return err
	}
	return persistChangedConfigAreas(DefaultConfig(), current, configPath)
}

// Update applies fn to the current config then saves
func Update(fn func(*Config)) error {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		return fmt.Errorf("%s", configText("cli.config.not_loaded"))
	}
	previous := cloneConfig(current)
	previousBranch := current.Server.Update.Branch
	fn(current)
	branch, ok := NormalizeUpdateBranch(current.Server.Update.Branch)
	if !ok {
		invalidBranch := current.Server.Update.Branch
		current.Server.Update.Branch = previousBranch
		return fmt.Errorf("%s", configText("cli.config.update_branch_invalid", "branch", invalidBranch))
	}
	current.Server.Update.Branch = branch
	if err := saveConfig(current, configPath); err != nil {
		return err
	}
	return persistChangedConfigAreas(previous, current, configPath)
}

func cloneConfig(cfg *Config) *Config {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return DefaultConfig()
	}
	clone := DefaultConfig()
	if yaml.Unmarshal(data, clone) != nil {
		return DefaultConfig()
	}
	clone.Server.Token = cfg.Server.Token
	return clone
}

func persistChangedConfigAreas(previous, current *Config, path string) error {
	areas := make([]string, 0, 13)
	if !reflect.DeepEqual(previous.Server.Branding, current.Server.Branding) || !reflect.DeepEqual(previous.Server.SEO, current.Server.SEO) || !reflect.DeepEqual(previous.Web.UI, current.Web.UI) {
		areas = append(areas, "branding")
	}
	if !reflect.DeepEqual(previous.Web, current.Web) || !reflect.DeepEqual(previous.Server.Healthz, current.Server.Healthz) || !reflect.DeepEqual(previous.Server.I18n, current.Server.I18n) {
		areas = append(areas, "web")
	}
	if !reflect.DeepEqual(previous.Server.Pages, current.Server.Pages) {
		areas = append(areas, "pages")
	}
	if !reflect.DeepEqual(previous.Server.Contact, current.Server.Contact) {
		areas = append(areas, "contact")
	}
	if !reflect.DeepEqual(previous.Server.Privacy, current.Server.Privacy) {
		areas = append(areas, "privacy")
	}
	if !reflect.DeepEqual(previous.Server.Security, current.Server.Security) || !reflect.DeepEqual(previous.Server.RateLimit, current.Server.RateLimit) || !reflect.DeepEqual(previous.Server.GeoIP, current.Server.GeoIP) || !reflect.DeepEqual(previous.Server.Tokens, current.Server.Tokens) || !reflect.DeepEqual(previous.Data, current.Data) {
		areas = append(areas, "security")
	}
	if !reflect.DeepEqual(previous.Server.Logging, current.Server.Logging) {
		areas = append(areas, "logs")
	}
	if !reflect.DeepEqual(previous.Server.Metrics, current.Server.Metrics) {
		areas = append(areas, "metrics")
	}
	if !reflect.DeepEqual(previous.Server.Notifications, current.Server.Notifications) {
		areas = append(areas, "notifications")
	}
	if !reflect.DeepEqual(previous.Server.Tor, current.Server.Tor) {
		areas = append(areas, "tor")
	}
	if !reflect.DeepEqual(previous.Server.I2P, current.Server.I2P) {
		areas = append(areas, "i2p")
	}
	if !reflect.DeepEqual(previous.Server.Maintenance, current.Server.Maintenance) || !reflect.DeepEqual(previous.Server.Schedule, current.Server.Schedule) || !reflect.DeepEqual(previous.Server.Update, current.Server.Update) || !reflect.DeepEqual(previous.Server.Backup, current.Server.Backup) || !reflect.DeepEqual(previous.CLI, current.CLI) {
		areas = append(areas, "maintenance")
	}
	for _, area := range areas {
		if err := writeConfigArea(path, area, current); err != nil {
			return err
		}
	}
	return nil
}

func writeConfigArea(configPath, area string, cfg *Config) error {
	var value interface{}
	switch area {
	case "branding":
		value = map[string]interface{}{"server": map[string]interface{}{"branding": cfg.Server.Branding, "seo": cfg.Server.SEO}, "web": map[string]interface{}{"ui": cfg.Web.UI}}
	case "web":
		value = map[string]interface{}{"server": map[string]interface{}{"healthz": cfg.Server.Healthz, "i18n": cfg.Server.I18n}, "web": cfg.Web}
	case "pages":
		pages := cfg.Server.Pages
		pages.Contact.CaptchaSecretKey = ""
		value = map[string]interface{}{"server": map[string]interface{}{"pages": pages}}
	case "contact":
		contact := cfg.Server.Contact
		for _, recipient := range []*ContactRecipientConfig{&contact.Admin, &contact.Security, &contact.Abuse, &contact.General, &contact.DPO} {
			recipient.Webhooks = nil
		}
		value = map[string]interface{}{"server": map[string]interface{}{"contact": contact}}
	case "privacy":
		value = map[string]interface{}{"server": map[string]interface{}{"privacy": cfg.Server.Privacy, "compliance": cfg.Server.Compliance, "tracking": cfg.Server.Tracking}}
	case "security":
		security := cfg.Server.Security
		security.EncryptionKey = ""
		data := cfg.Data
		data.CVE.APIKey = ""
		value = map[string]interface{}{"server": map[string]interface{}{"security": security, "rate_limit": cfg.Server.RateLimit, "geoip": cfg.Server.GeoIP, "tokens": cfg.Server.Tokens}, "data": data}
	case "logs":
		value = map[string]interface{}{"server": map[string]interface{}{"logs": cfg.Server.Logging}}
	case "metrics":
		metrics := cfg.Server.Metrics
		metrics.Auth.Tokens = MetricsTokenConfig{}
		value = map[string]interface{}{"server": map[string]interface{}{"metrics": metrics}}
	case "notifications":
		notifications := cfg.Server.Notifications
		notifications.Email.SMTP.Password = ""
		value = map[string]interface{}{"server": map[string]interface{}{"notifications": notifications}}
	case "tor":
		value = map[string]interface{}{"server": map[string]interface{}{"tor": cfg.Server.Tor}}
	case "i2p":
		value = map[string]interface{}{"server": map[string]interface{}{"i2p": cfg.Server.I2P}}
	case "maintenance":
		value = map[string]interface{}{"server": map[string]interface{}{"maintenance": cfg.Server.Maintenance, "scheduler": cfg.Server.Schedule, "update": cfg.Server.Update, "backup": cfg.Server.Backup}, "cli": cfg.CLI}
	default:
		return fmt.Errorf("%s", configText("cli.config.area_unknown", "area", area))
	}
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(encoded, &document); err != nil || len(document.Content) == 0 {
		return fmt.Errorf("%s", configText("cli.config.area_encode_failed"))
	}
	for _, secretPath := range [][]string{
		{"server", "security", "encryption_key"},
		{"server", "metrics", "auth", "tokens"},
		{"server", "notifications", "email", "smtp", "password"},
		{"server", "pages", "contact", "captcha_secret_key"},
		{"server", "ssl", "letsencrypt", "dns_credentials"},
		{"server", "backup", "encryption_password"},
		{"server", "database", "token"},
		{"server", "cache", "password"},
		{"server", "database", "url"},
		{"server", "cache", "url"},
		{"data", "cve", "api_key"},
	} {
		removeConfigPath(document.Content[0], secretPath)
	}
	for _, role := range []string{"admin", "security", "abuse", "general"} {
		removeConfigPath(document.Content[0], []string{"server", "contact", role, "webhooks"})
	}
	encoded, err = yaml.Marshal(&document)
	if err != nil {
		return err
	}
	directory := filepath.Join(filepath.Dir(configPath), "yaml.d")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return err
	}
	filename := areaConfigFiles[area]
	content := append([]byte("# Configuration area (AI.md PART 5).\n"), encoded...)
	return writeConfigAtomically(filepath.Join(directory, filename), content)
}

// NormalizeUpdateBranch maps supported channel aliases to their canonical name.
func NormalizeUpdateBranch(branch string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(branch)) {
	case "", "stable":
		return "stable", true
	case "beta":
		return "beta", true
	case "devel", "daily", "rolling", "dev", "development":
		return "devel", true
	default:
		return "", false
	}
}

func saveConfig(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.directory_create_failed"), err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.directory_secure_failed"), err)
	}
	if cfg.Server.Security.EncryptionKey != "" || cfg.Server.Backup.EncryptionPassword != "" || len(cfg.Server.SSL.LetsEncrypt.DNSCredentials) > 0 {
		secrets, err := readSecrets(filepath.Join(dir, "secrets.yml"))
		if err != nil {
			return err
		}
		server := nestedSecretMap(secrets, "server")
		if cfg.Server.Token != "" {
			server["token"] = cfg.Server.Token
		}
		if cfg.Server.Security.EncryptionKey != "" {
			security := nestedSecretMap(server, "security")
			security["encryption_key"] = cfg.Server.Security.EncryptionKey
			server["security"] = security
		}
		if cfg.Server.Backup.EncryptionPassword != "" {
			backup := nestedSecretMap(server, "backup")
			backup["encryption_password"] = cfg.Server.Backup.EncryptionPassword
			server["backup"] = backup
		}
		if cfg.Server.Database.Token != "" || urlContainsCredentials(cfg.Server.Database.URL) {
			database := nestedSecretMap(server, "database")
			if cfg.Server.Database.Token != "" {
				database["token"] = cfg.Server.Database.Token
			}
			if urlContainsCredentials(cfg.Server.Database.URL) {
				database["url"] = cfg.Server.Database.URL
			}
			server["database"] = database
		}
		if cfg.Server.Cache.Password != "" || urlContainsCredentials(cfg.Server.Cache.URL) {
			cache := nestedSecretMap(server, "cache")
			if cfg.Server.Cache.Password != "" {
				cache["password"] = cfg.Server.Cache.Password
			}
			if urlContainsCredentials(cfg.Server.Cache.URL) {
				cache["url"] = cfg.Server.Cache.URL
			}
			server["cache"] = cache
		}
		if len(cfg.Server.SSL.LetsEncrypt.DNSCredentials) > 0 {
			ssl := nestedSecretMap(server, "ssl")
			letsencrypt := nestedSecretMap(ssl, "letsencrypt")
			credentials := make(map[string]interface{}, len(cfg.Server.SSL.LetsEncrypt.DNSCredentials))
			for name, value := range cfg.Server.SSL.LetsEncrypt.DNSCredentials {
				if strings.HasPrefix(value, "enc:v1:") {
					credentials[name] = value
					continue
				}
				encrypted, err := encryptDNSCredential(cfg.Server.Security.EncryptionKey, value)
				if err != nil {
					return fmt.Errorf("cannot protect DNS credential %s: %w", name, err)
				}
				credentials[name] = encrypted
			}
			letsencrypt["dns_credentials"] = credentials
			ssl["letsencrypt"] = letsencrypt
			server["ssl"] = ssl
		}
		secrets["server"] = server
		if err := writeSecrets(filepath.Join(dir, "secrets.yml"), secrets); err != nil {
			return err
		}
	}

	content := generateConfigYAML(cfg)
	if err := writeConfigAtomically(path, []byte(content)); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.file_write_failed"), err)
	}
	return nil
}

func validateBackupConfig(cfg *Config) {
	cfg.Server.Backup.Encryption.Enabled = cfg.Server.Backup.EncryptionPassword != ""
}

func validateRestoreConfig(cfg *Config) {
	defaults := DefaultConfig().Server.Restore
	checkSize := func(value *string, fallback, key string) {
		if validByteSize(*value) {
			return
		}
		*value = fallback
		fmt.Fprintln(os.Stderr, configText("cli.config.invalid_value_warning", "file", "yaml.d/maintenance.yml", "line", "?", "key", key))
	}
	checkSize(&cfg.Server.Restore.MaxArchiveSize, defaults.MaxArchiveSize, "server.restore.max_archive_size")
	checkSize(&cfg.Server.Restore.MaxExpandedSize, defaults.MaxExpandedSize, "server.restore.max_expanded_size")
	checkSize(&cfg.Server.Restore.MaxFileSize, defaults.MaxFileSize, "server.restore.max_file_size")
	if cfg.Server.Restore.MaxFiles < 1 {
		cfg.Server.Restore.MaxFiles = defaults.MaxFiles
		fmt.Fprintln(os.Stderr, configText("cli.config.invalid_value_warning", "file", "yaml.d/maintenance.yml", "line", "?", "key", "server.restore.max_files"))
	}
	if cfg.Server.Restore.MaxRatio < 1 {
		cfg.Server.Restore.MaxRatio = defaults.MaxRatio
		fmt.Fprintln(os.Stderr, configText("cli.config.invalid_value_warning", "file", "yaml.d/maintenance.yml", "line", "?", "key", "server.restore.max_ratio"))
	}
}

func validateDebugConfig(cfg *Config) {
	defaults := DefaultConfig().Server.Debug
	debug := &cfg.Server.Debug
	if !validDebugBodyLogSize(debug.MaxBodyLogSize) {
		debug.MaxBodyLogSize = defaults.MaxBodyLogSize
		warnInvalidConfigKey("server.debug.max_body_log_size")
	}
	if debug.BlockProfileRate < 0 {
		debug.BlockProfileRate = defaults.BlockProfileRate
		warnInvalidConfigKey("server.debug.block_profile_rate")
	}
	if debug.MutexProfileFraction < 0 {
		debug.MutexProfileFraction = defaults.MutexProfileFraction
		warnInvalidConfigKey("server.debug.mutex_profile_fraction")
	}
}

func validateLoggingConfig(cfg *Config) {
	level := strings.ToLower(strings.TrimSpace(cfg.Server.Logging.Level))
	if level != "" && level != "debug" && level != "info" && level != "warn" && level != "error" {
		level = ""
		warnInvalidConfigKey("server.logs.level")
	}
	cfg.Server.Logging.Level = level
	defaults := DefaultConfig().Server.Logging
	validateLog := func(name string, current *LogFileConfig, fallback LogFileConfig, allowed ...string) {
		if strings.TrimSpace(current.Format) == "" {
			current.Format = fallback.Format
		}
		format := strings.ToLower(strings.TrimSpace(current.Format))
		valid := false
		for _, candidate := range allowed {
			if format == candidate {
				valid = true
				break
			}
		}
		if !valid {
			current.Format = fallback.Format
			warnInvalidConfigKey("server.logs." + name + ".format")
		} else {
			current.Format = format
		}
		if current.Filename != "" && (filepath.Base(current.Filename) != current.Filename || strings.ContainsAny(current.Filename, "\\/")) {
			current.Filename = ""
			warnInvalidConfigKey("server.logs." + name + ".filename")
		}
	}
	validateLog("access", &cfg.Server.Logging.Access, defaults.Access, "apache", "nginx", "json", "custom")
	validateLog("server", &cfg.Server.Logging.Server, defaults.Server, "text", "json")
	validateLog("error", &cfg.Server.Logging.Error, defaults.Error, "text", "json")
	validateLog("audit", &cfg.Server.Logging.Audit.LogFileConfig, defaults.Audit.LogFileConfig, "json")
	validateLog("security", &cfg.Server.Logging.Security, defaults.Security, "fail2ban", "syslog", "cef", "json", "text")
	validateLog("debug", &cfg.Server.Logging.Debug, defaults.Debug, "text", "json")
	if cfg.Server.Logging.Defaults.Custom != "" && !validCustomLogFormat(cfg.Server.Logging.Defaults.Custom) {
		cfg.Server.Logging.Defaults.Custom = defaults.Defaults.Custom
		warnInvalidConfigKey("server.logs.defaults.custom")
	}
	if cfg.Server.Logging.Access.Format == "custom" {
		custom := cfg.Server.Logging.Access.Custom
		if custom == "" {
			custom = cfg.Server.Logging.Defaults.Custom
		}
		if !validCustomLogFormat(custom) {
			cfg.Server.Logging.Access.Format = defaults.Access.Format
			cfg.Server.Logging.Access.Custom = ""
			warnInvalidConfigKey("server.logs.access.custom")
		}
	}
	if !validLogRotation(cfg.Server.Logging.Defaults.Rotate) {
		cfg.Server.Logging.Defaults.Rotate = defaults.Defaults.Rotate
		warnInvalidConfigKey("server.logs.defaults.rotate")
	}
	if !validLogRetention(cfg.Server.Logging.Defaults.Keep) {
		cfg.Server.Logging.Defaults.Keep = defaults.Defaults.Keep
		warnInvalidConfigKey("server.logs.defaults.keep")
	}
	for name, settings := range map[string]*LogFileConfig{
		"access": &cfg.Server.Logging.Access, "server": &cfg.Server.Logging.Server,
		"error": &cfg.Server.Logging.Error, "audit": &cfg.Server.Logging.Audit.LogFileConfig,
		"security": &cfg.Server.Logging.Security, "debug": &cfg.Server.Logging.Debug,
	} {
		if settings.Rotate != "" && !validLogRotation(settings.Rotate) {
			settings.Rotate = ""
			warnInvalidConfigKey("server.logs." + name + ".rotate")
		}
		if settings.Keep != "" && !validLogRetention(settings.Keep) {
			settings.Keep = ""
			warnInvalidConfigKey("server.logs." + name + ".keep")
		}
	}
}

func validCustomLogFormat(format string) bool {
	allowed := map[string]bool{
		"time": true, "date": true, "datetime": true, "remote_ip": true,
		"method": true, "path": true, "query": true, "status": true, "bytes": true,
		"latency": true, "latency_ms": true, "user_agent": true, "referer": true,
		"request_id": true, "fqdn": true, "protocol": true, "tls_version": true,
		"country": true, "asn": true,
	}
	for index := 0; index < len(format); {
		if format[index] == '{' {
			end := strings.IndexByte(format[index:], '}')
			if end < 0 {
				return false
			}
			if !allowed[format[index+1:index+end]] {
				return false
			}
			index += end + 1
			continue
		}
		if format[index] == '}' {
			return false
		}
		index++
	}
	return strings.TrimSpace(format) != ""
}

func validLogRotation(value string) bool {
	for _, part := range strings.Split(strings.ToLower(strings.TrimSpace(value)), ",") {
		part = strings.TrimSpace(part)
		if part == "daily" || part == "weekly" || part == "monthly" || part == "yearly" || part == "never" {
			continue
		}
		if !validLogSize(part) {
			return false
		}
	}
	return strings.TrimSpace(value) != ""
}

func validLogSize(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, suffix := range []string{"TIB", "GIB", "MIB", "KIB", "TB", "GB", "MB", "KB", "T", "G", "M", "K", "B"} {
		if strings.HasSuffix(value, suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(value, suffix))
			parsed, err := strconv.ParseUint(number, 10, 64)
			return err == nil && parsed > 0
		}
	}
	return false
}

func validLogRetention(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" || value == "forever" {
		return true
	}
	for _, part := range strings.Split(value, ":") {
		if part == "daily" || part == "weekly" || part == "monthly" || part == "yearly" {
			continue
		}
		if part == "" {
			return false
		}
		for _, suffix := range []string{"d", "w", "m"} {
			if strings.HasSuffix(part, suffix) {
				part = strings.TrimSuffix(part, suffix)
				break
			}
		}
		if _, err := strconv.ParseUint(part, 10, 32); err == nil {
			continue
		}
		return false
	}
	return true
}

func validateNotificationsConfig(cfg *Config) {
	webuiDefaults := DefaultNotificationsConfig().WebUI
	webui := &cfg.Server.Notifications.WebUI
	if webui.Duration < 0 {
		webui.Duration = webuiDefaults.Duration
		warnInvalidConfigKey("server.notifications.webui.duration")
	}
	if webui.WarningDuration < 0 {
		webui.WarningDuration = webuiDefaults.WarningDuration
		warnInvalidConfigKey("server.notifications.webui.warning_duration")
	}
	defaults := DefaultNotificationsConfig().DatabaseError
	settings := &cfg.Server.Notifications.DatabaseError
	if settings.Failures < 1 {
		settings.Failures = defaults.Failures
		warnInvalidConfigKey("server.notifications.database_error.failures")
	}
	for key, value := range map[string]string{"check_interval": settings.CheckInterval, "repeat": settings.Repeat} {
		duration, err := time.ParseDuration(strings.TrimSpace(value))
		if err == nil && duration > 0 {
			continue
		}
		if key == "check_interval" {
			settings.CheckInterval = defaults.CheckInterval
		} else {
			settings.Repeat = defaults.Repeat
		}
		warnInvalidConfigKey("server.notifications.database_error." + key)
	}
}

func validDebugBodyLogSize(value string) bool {
	value = strings.TrimSpace(strings.ToUpper(value))
	units := []struct {
		suffix     string
		multiplier uint64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"TIB", 1 << 40},
		{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30}, {"TB", 1 << 40},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"T", 1 << 40}, {"B", 1},
	}
	var multiplier uint64 = 1
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
			multiplier = unit.multiplier
			break
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0 && n <= uint64(^uint(0)>>1)/multiplier
}

func validateAbuseDetectionConfig(cfg *Config) {
	defaults := DefaultConfig().Server.Security.AbuseDetection
	flood := &cfg.Server.Security.AbuseDetection.RequestFlood
	if flood.Multiplier < 1 {
		flood.Multiplier = defaults.RequestFlood.Multiplier
		warnInvalidConfigKey("server.security.abuse_detection.request_flood.multiplier")
	}
	if duration, err := time.ParseDuration(flood.Window); err != nil || duration <= 0 {
		flood.Window = defaults.RequestFlood.Window
		warnInvalidConfigKey("server.security.abuse_detection.request_flood.window")
	}
	if duration, err := time.ParseDuration(flood.BlockDuration); err != nil || duration <= 0 {
		flood.BlockDuration = defaults.RequestFlood.BlockDuration
		warnInvalidConfigKey("server.security.abuse_detection.request_flood.block_duration")
	}
}

func validateTorConfig(cfg *Config) {
	defaults := DefaultConfig().Server.Tor
	tor := &cfg.Server.Tor
	if tor.StartupTimeout < 10 || tor.StartupTimeout > 300 {
		tor.StartupTimeout = defaults.StartupTimeout
		warnInvalidConfigKey("server.tor.startup_timeout")
	}
	if tor.NumIntroPoints < 3 || tor.NumIntroPoints > 10 {
		tor.NumIntroPoints = defaults.NumIntroPoints
		warnInvalidConfigKey("server.tor.num_intro_points")
	}
}

func validByteSize(value string) bool {
	value = strings.TrimSpace(strings.ToUpper(value))
	for _, suffix := range []string{"KIB", "MIB", "GIB", "TIB", "KB", "MB", "GB", "TB", "K", "M", "G", "T", "B"} {
		value = strings.TrimSuffix(value, suffix)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return err == nil && n > 0
}

func applyI2PEnvironment(cfg *Config) error {
	value, ok := os.LookupEnv("I2P_ENABLED")
	if !ok {
		return nil
	}
	defaultEnabled := DefaultConfig().Server.I2P.Enabled
	enabled, err := ParseBool(value, defaultEnabled)
	if err != nil {
		cfg.Server.I2P.Enabled = defaultEnabled
		fmt.Fprintln(os.Stderr, configText("cli.config.i2p_env_invalid_warning"))
		return nil
	}
	cfg.Server.I2P.Enabled = enabled
	return nil
}

func seedFirstRunConnections(cfg *Config) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if cfg.Server.Database.URL == "" && databaseURL != "" {
		cfg.Server.Database.URL = databaseURL
	} else if cfg.Server.Database.URL == "" {
		if driver := strings.TrimSpace(os.Getenv("DATABASE_DRIVER")); driver != "" {
			cfg.Server.Database.Driver = driver
		}
	}
	cacheURL := strings.TrimSpace(os.Getenv("CACHE_URL"))
	if cfg.Server.Cache.URL == "" && cacheURL != "" {
		cfg.Server.Cache.URL = cacheURL
	}
}

func warnIgnoredConnectionEnvironment(cfg *Config) {
	for _, entry := range []struct {
		name  string
		saved string
	}{
		{name: "DATABASE_URL", saved: cfg.Server.Database.URL},
		{name: "CACHE_URL", saved: cfg.Server.Cache.URL},
	} {
		value := strings.TrimSpace(os.Getenv(entry.name))
		saved := strings.TrimSpace(entry.saved)
		if value != "" && saved != "" && !strings.Contains(saved, "${") && value != saved {
			fmt.Fprintln(os.Stderr, configText("cli.config.connection_env_ignored_warning", "name", entry.name))
		}
	}
	if value := strings.TrimSpace(os.Getenv("DATABASE_DRIVER")); value != "" && cfg.Server.Database.URL == "" && normalizeDatabaseDriver(value) != normalizeDatabaseDriver(cfg.Server.Database.Driver) {
		fmt.Fprintln(os.Stderr, configText("cli.config.database_driver_env_ignored_warning"))
	}
}

func normalizeDatabaseDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "sqlite", "sqlite2", "sqlite3":
		return "sqlite"
	case "libsql", "turso":
		return "libsql"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

func applyConnectionURLTypes(cfg *Config) {
	if cfg.Server.Database.URL != "" {
		parsed, err := url.Parse(strings.TrimSpace(cfg.Server.Database.URL))
		if err == nil {
			driver := ""
			switch strings.ToLower(parsed.Scheme) {
			case "libsql", "https":
				driver = "libsql"
			case "", "file", "sqlite", "sqlite2", "sqlite3":
				driver = "sqlite"
			}
			if driver != "" {
				if normalizeDatabaseDriver(cfg.Server.Database.Driver) != driver {
					fmt.Fprintln(os.Stderr, configText("cli.config.database_url_scheme_warning"))
				}
				cfg.Server.Database.Driver = driver
			}
		}
	}
	if cfg.Server.Cache.URL != "" {
		parsed, err := url.Parse(strings.TrimSpace(cfg.Server.Cache.URL))
		if err == nil {
			switch strings.ToLower(parsed.Scheme) {
			case "valkey", "valkeys":
				cfg.Server.Cache.Type = "valkey"
			case "redis", "rediss":
				cfg.Server.Cache.Type = "redis"
			}
		}
	}
}

func generateConfigYAML(cfg *Config) string {
	node := struct {
		Server struct {
			Port           string               `yaml:"port"`
			Address        string               `yaml:"address"`
			FQDN           string               `yaml:"fqdn"`
			Mode           string               `yaml:"mode"`
			APIVersion     string               `yaml:"api_version"`
			BaseURL        string               `yaml:"baseurl"`
			PIDFile        bool                 `yaml:"pidfile"`
			Daemonize      bool                 `yaml:"daemonize"`
			Database       DatabaseConfig       `yaml:"database"`
			Cache          CacheConfig          `yaml:"cache"`
			TrustedProxies TrustedProxiesConfig `yaml:"trusted_proxies"`
		} `yaml:"server"`
	}{}
	node.Server.Port = cfg.Server.Port
	node.Server.Address = cfg.Server.Address
	node.Server.FQDN = cfg.Server.FQDN
	node.Server.Mode = cfg.Server.Mode
	node.Server.APIVersion = cfg.Server.APIVersion
	node.Server.BaseURL = cfg.Server.BaseURL
	node.Server.PIDFile = cfg.Server.PIDFile
	node.Server.Daemonize = cfg.Server.Daemonize
	node.Server.Database = cfg.Server.Database
	node.Server.Cache = cfg.Server.Cache
	node.Server.Database.Token = ""
	node.Server.Cache.Password = ""
	if urlContainsCredentials(node.Server.Database.URL) {
		node.Server.Database.URL = ""
	}
	if urlContainsCredentials(node.Server.Cache.URL) {
		node.Server.Cache.URL = ""
	}
	node.Server.TrustedProxies = cfg.Server.TrustedProxies
	encoded, err := yaml.Marshal(node)
	if err != nil {
		return "# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\nserver: {}\n"
	}
	return "# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n" + string(encoded)
}

func ensureEncryptionKey(cfg *Config) error {
	if strings.TrimSpace(cfg.Server.Security.EncryptionKey) != "" {
		key, err := base64.StdEncoding.DecodeString(cfg.Server.Security.EncryptionKey)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("%s", configText("cli.config.encryption_key_invalid"))
		}
		return nil
	}
	key := make([]byte, 32)
	if _, err := cryptorand.Read(key); err != nil {
		return fmt.Errorf("%s: %w", configText("cli.config.encryption_key_generate_failed"), err)
	}
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString(key)
	return nil
}

func validateContactCaptcha(cfg *Config) error {
	contact := cfg.Server.Pages.Contact
	provider := strings.ToLower(strings.TrimSpace(contact.Captcha))
	switch provider {
	case "", "simple":
		return nil
	case "recaptcha", "hcaptcha":
		if strings.TrimSpace(contact.CaptchaSiteKey) == "" || strings.TrimSpace(contact.CaptchaSecretKey) == "" {
			cfg.Server.Pages.Contact.Captcha = DefaultConfig().Server.Pages.Contact.Captcha
			warnInvalidConfigKey("server.pages.contact.captcha")
		}
		return nil
	default:
		cfg.Server.Pages.Contact.Captcha = DefaultConfig().Server.Pages.Contact.Captcha
		warnInvalidConfigKey("server.pages.contact.captcha")
		return nil
	}
}

func validateClientIPHeader(cfg *Config) error {
	value := cfg.Server.TrustedProxies.ClientIPHeader
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "auto", "x-forwarded-for", "forwarded", "cf-connecting-ip", "true-client-ip", "x-real-ip", "x-client-ip":
		return nil
	default:
		cfg.Server.TrustedProxies.ClientIPHeader = DefaultConfig().Server.TrustedProxies.ClientIPHeader
		warnInvalidConfigKey("server.trusted_proxies.client_ip_header")
		return nil
	}
}

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
      # warning toast seconds (0 = manual dismiss; errors always require manual dismissal)
      warning_duration: %d
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
        update_failed: %t
        update_staged: %t
        database_error: %t
    disk_low:
      enabled: %t
      threshold_percent: %.1f
      check_interval: %s
      repeat: %s
    database_error:
      enabled: %t
      failures: %d
      check_interval: %s
      repeat: %s

`,
		n.WebUI.Position,
		n.WebUI.Duration,
		n.WebUI.WarningDuration,
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
		e.Events.UpdateFailed,
		e.Events.UpdateStaged,
		e.Events.DatabaseError,
		n.DiskLow.Enabled,
		n.DiskLow.ThresholdPercent,
		n.DiskLow.CheckInterval,
		n.DiskLow.Repeat,
		n.DatabaseError.Enabled,
		n.DatabaseError.Failures,
		n.DatabaseError.CheckInterval,
		n.DatabaseError.Repeat,
	)
}

// randomPort selects a random unused port in the 64000-64999 range
func randomPort() int {
	for attempts := 0; attempts < 100; attempts++ {
		port := 64000 + rand.Intn(1000)
		addr := fmt.Sprintf(":%d", port)
		l, err := net.Listen("tcp", addr)
		if err == nil {
			if err := l.Close(); err != nil {
				continue
			}
			return port
		}
	}
	return 64580 // fallback
}

func defaultServerPort() string {
	if apppath.IsRunningInContainer() {
		return "80"
	}
	return fmt.Sprintf("%d", randomPort())
}

// DefaultPort selects the container listener port or a free native 64xxx port.
func DefaultPort() string {
	return defaultServerPort()
}

// UnprivilegedDefaultPort selects an unused port from the native 64xxx range.
func UnprivilegedDefaultPort() string {
	return fmt.Sprintf("%d", randomPort())
}

// NormalizePort validates the configured TCP listening port.
func NormalizePort(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) < 1 || len(parts) > 2 {
		return "", false
	}
	for index, part := range parts {
		port, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || port < 1 || port > 65535 {
			return "", false
		}
		parts[index] = strconv.Itoa(port)
	}
	return strings.Join(parts, ","), true
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

	return false, fmt.Errorf("%s", configText("cli.config.boolean_invalid", "value", s))
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
