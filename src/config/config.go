package config

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
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
	Server      ServerConfig      `yaml:"server"`
	Tor         TorIdentityConfig `yaml:"tor"`
	Web         WebConfig         `yaml:"web"`
	WebRobots   WebRobotsConfig   `yaml:"web_robots"`
	WebSecurity WebSecurityConfig `yaml:"web_security"`
	Data        DataConfig        `yaml:"data"`
}

// TorIdentityConfig contains public identity used to detect and render onion requests.
type TorIdentityConfig struct {
	OnionAddress string `yaml:"onion_address"`
	ContactEmail string `yaml:"contact_email"`
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
	User           string               `yaml:"user"`
	Group          string               `yaml:"group"`
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
		Text string `yaml:"text"`
		URL  string `yaml:"url"`
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
	EncryptionKey string                `yaml:"encryption_key"`
	Reports       SecurityReportsConfig `yaml:"reports"`
	Blocklists    BlocklistsConfig      `yaml:"blocklists"`
	Allowlist     []AllowlistEntry      `yaml:"allowlist"`
	BlockedIPs    []BlockedIPEntry      `yaml:"blocked_ips"`
}

type AllowlistEntry struct {
	CIDR        string `yaml:"cidr"`
	Description string `yaml:"description"`
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
	Branch      string `yaml:"branch"`
	AutoInstall bool   `yaml:"auto_install"`
	DeferDays   int    `yaml:"defer_days"`
}

// NotificationsConfig contains the operator/visitor notification settings
// (AI.md PART 17). The public WebUI toast settings are visitor-facing; Email
// carries the SMTP + template configuration used for operator alerts.
type NotificationsConfig struct {
	WebUI   WebUINotifyConfig `yaml:"webui"`
	Email   EmailConfig       `yaml:"email"`
	DiskLow DiskLowConfig     `yaml:"disk_low"`
}

type DiskLowConfig struct {
	Enabled          bool    `yaml:"enabled"`
	ThresholdPercent float64 `yaml:"threshold_percent"`
	CheckInterval    string  `yaml:"check_interval"`
	Repeat           string  `yaml:"repeat"`
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
		DiskLow: DiskLowConfig{Enabled: true, ThresholdPercent: 85, CheckInterval: "5m", Repeat: "24h"},
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
	Title       string                    `yaml:"title"`
	Tagline     string                    `yaml:"tagline"`
	Description string                    `yaml:"description"`
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
	Keywords []string `yaml:"keywords"`
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
	Timezone      string `yaml:"timezone"`
	CatchUpWindow string `yaml:"catch_up_window"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	Enabled  bool `yaml:"enabled"`
	Requests int  `yaml:"requests"`
	Window   int  `yaml:"window"`
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
	Driver   string `yaml:"driver"`
	URL      string `yaml:"url,omitempty"`
	Token    string `yaml:"token,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Name     string `yaml:"name,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	SSLMode  string `yaml:"sslmode,omitempty"`
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
	Encryption         BackupEncryptionConfig `yaml:"encryption"`
	EncryptionPassword string                 `yaml:"encryption_password"`
}

// BackupEncryptionConfig controls whether backup encryption is configured.
type BackupEncryptionConfig struct {
	Enabled bool `yaml:"enabled"`
}

// WebConfig contains frontend/web settings
type WebConfig struct {
	UI         WebUIConfig       `yaml:"ui"`
	Reports    WebReportsConfig  `yaml:"reports"`
	CORS       string            `yaml:"cors"`
	Security   WebSecurityConfig `yaml:"security"`
	APIContact string            `yaml:"api_contact"`
	LLMS       LLMSConfig        `yaml:"llms"`
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
	Theme string `yaml:"theme"`
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
			User:       "gitignore",
			Group:      "gitignore",
			PIDFile:    true,
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
			Privacy: DefaultPrivacyConfig(),
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
			},
			Cache: CacheConfig{
				Type: "memory", Host: "localhost", Port: 6379, TLSSkipVerify: true,
				PoolSize: 10, MinIdle: 2, Timeout: "5s", Prefix: "gitignore:", TTL: "1h",
			},
			Logging: LoggingConfig{
				Level:    "warn",
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
				EncryptionPassword: "",
			},
			Compliance: ComplianceConfig{Enabled: false},
			I18n: I18nConfig{
				Enabled:            true,
				DefaultLanguage:    "en",
				FallbackLanguage:   "en",
				AvailableLanguages: []string{"en", "es", "fr", "ar", "de", "it", "zh", "ja"},
				CookieName:         "lang",
				CookieMaxAge:       "365d",
			},
			Tor:           DefaultTorConfig(),
			I2P:           DefaultI2PConfig(),
			Tokens:        OwnerTokensConfig{MaxLinked: 25, CookieMaxAge: "1y", IdlePurgeDays: 0},
			GeoIP:         DefaultGeoIPConfig(),
			Notifications: DefaultNotificationsConfig(),
			Security: ServerSecurityConfig{
				Reports:    SecurityReportsConfig{RetentionDays: 365},
				Blocklists: BlocklistsConfig{Enabled: true, ExtraSources: []BlocklistSourceConfig{}},
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
			UI: WebUIConfig{
				Theme: "dark",
			},
			Reports: WebReportsConfig{
				RateLimitPerMinute:  60,
				RateLimitPerIPBurst: 10,
			},
			CORS: "*",
			LLMS: LLMSConfig{IncludeEndpoints: true},
			Security: WebSecurityConfig{
				PublishPGPKey: true,
				Keyservers:    []string{},
			},
		},
		WebRobots: WebRobotsConfig{
			Allow: []string{"/", "/api"},
			Deny:  []string{"/debug"},
		},
		WebSecurity: WebSecurityConfig{
			Admin: "",
			CORS:  "*",
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
		warnIgnoredConnectionEnvironment(cfg)
		applyConnectionURLTypes(cfg)
		if err := applyI2PEnvironment(cfg); err != nil {
			return nil, err
		}
		if _, err := loadSensitiveSecrets(cfg, path); err != nil {
			return nil, err
		}
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
	if (needsSecurityKey || legacySecurityKey) && !mainConfigInvalid {
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
	if branch != cfg.Server.Update.Branch && !mainConfigInvalid {
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
		cfg.Server.Port = fmt.Sprintf("%d", randomPort())
		if !mainConfigInvalid {
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
	if legacySchedule := mappingValue(server, "schedule"); legacySchedule != nil && legacySchedule.Kind == yaml.MappingNode {
		for index := len(legacySchedule.Content) - 2; index >= 0; index -= 2 {
			key := legacySchedule.Content[index].Value
			if key != "timezone" && key != "catch_up_window" {
				legacySchedule.Content = append(legacySchedule.Content[:index], legacySchedule.Content[index+2:]...)
			}
		}
	}
	changed := moveConfigMapping(server, "schedule", "scheduler")
	changed = moveConfigMapping(server, "logging", "logs") || changed
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
	areaDir := filepath.Join(configDir, "yaml.d")
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
	defaults := reflect.ValueOf(DefaultConfig()).Elem()
	target := reflect.ValueOf(current).Elem()
	applyConfigMapping(path, document.Content[0], target, defaults, "")
	lastGoodConfigYAML[configFileCacheKey(path)] = append([]byte(nil), data...)
	return nil
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
		fieldIndex := configFieldIndex(target.Type(), key)
		if fieldIndex < 0 {
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
	if !reflect.DeepEqual(previous.Web, current.Web) || !reflect.DeepEqual(previous.WebSecurity, current.WebSecurity) || !reflect.DeepEqual(previous.WebRobots, current.WebRobots) || !reflect.DeepEqual(previous.Server.Healthz, current.Server.Healthz) || !reflect.DeepEqual(previous.Server.I18n, current.Server.I18n) {
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
	if !reflect.DeepEqual(previous.Server.Maintenance, current.Server.Maintenance) || !reflect.DeepEqual(previous.Server.Schedule, current.Server.Schedule) || !reflect.DeepEqual(previous.Server.Update, current.Server.Update) || !reflect.DeepEqual(previous.Server.Backup, current.Server.Backup) {
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
		value = map[string]interface{}{"server": map[string]interface{}{"healthz": cfg.Server.Healthz, "i18n": cfg.Server.I18n}, "web": cfg.Web, "web_robots": cfg.WebRobots, "web_security": cfg.WebSecurity}
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
		value = map[string]interface{}{"server": map[string]interface{}{"maintenance": cfg.Server.Maintenance, "scheduler": cfg.Server.Schedule, "update": cfg.Server.Update, "backup": cfg.Server.Backup}}
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
	if cfg.Server.Security.EncryptionKey != "" || cfg.Server.Backup.EncryptionPassword != "" {
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

func applyI2PEnvironment(cfg *Config) error {
	value, ok := os.LookupEnv("I2P_ENABLED")
	if !ok {
		return nil
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		cfg.Server.I2P.Enabled = DefaultConfig().Server.I2P.Enabled
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
    disk_low:
      enabled: %t
      threshold_percent: %.1f
      check_interval: %s
      repeat: %s

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
		n.DiskLow.Enabled,
		n.DiskLow.ThresholdPercent,
		n.DiskLow.CheckInterval,
		n.DiskLow.Repeat,
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
