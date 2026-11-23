package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/apimgr/gitignore/src/blocklist"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/geoip"
	graphapi "github.com/apimgr/gitignore/src/graphql"
	"github.com/apimgr/gitignore/src/health"
	"github.com/apimgr/gitignore/src/mode"
	"github.com/apimgr/gitignore/src/notify"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/server/metrics"
	"github.com/apimgr/gitignore/src/ssl"
	"github.com/apimgr/gitignore/src/template"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// apiVersion returns the configured API version, falling back to v1.
func (s *Server) apiVersion() string {
	if s.config != nil && s.currentConfig() != nil {
		if version := strings.Trim(strings.TrimSpace(s.currentConfig().Server.APIVersion), "/"); version != "" {
			return version
		}
	}
	return "v1"
}

// SchedulerMetricsObserver adapts scheduler state changes to the internal
// metrics registry without coupling the scheduler package to the server.
func (s *Server) SchedulerMetricsObserver() func(string, bool, string, time.Duration, time.Time) {
	if s.metrics == nil {
		return nil
	}
	return func(id string, running bool, status string, duration time.Duration, lastRun time.Time) {
		if running {
			s.metrics.SchedulerTasksRunning.WithLabelValues(id).Set(1)
			return
		}
		s.metrics.SchedulerTasksRunning.WithLabelValues(id).Set(0)
		if status == "" {
			return
		}
		s.metrics.SchedulerTasksTotal.WithLabelValues(id, status).Inc()
		s.metrics.SchedulerTaskDuration.WithLabelValues(id).Observe(duration.Seconds())
		s.metrics.SchedulerLastRun.WithLabelValues(id).Set(float64(lastRun.Unix()))
	}
}

// apiBasePath returns the configured versioned API prefix.
func (s *Server) apiBasePath() string {
	return "/api/" + s.apiVersion()
}

func parseSecurityPrefix(value string) (netip.Prefix, bool) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), true
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, false
	}
	address = address.Unmap()
	return netip.PrefixFrom(address, address.BitLen()), true
}

func overlyBroadSecurityPrefix(prefix netip.Prefix) bool {
	if prefix.Addr().Is4() {
		return prefix.Bits() <= 7
	}
	return prefix.Bits() <= 15
}

// Config holds server configuration
type Config struct {
	Address           string
	Port              int
	DevMode           bool
	Templates         *template.Manager
	Paths             *apppath.PathManager
	Version           string
	Commit            string
	BuildDate         string
	Cfg               *config.Config
	ConfigManager     *config.ConfigManager
	GeoIP             *geoip.Manager
	SendContact       func(to, subject, body string) error
	SendContactReply  func(to, replyTo, subject, body string) error
	WebhookDispatcher *notify.Dispatcher
	I2PStatus         func() I2PInfo
	Blocklist         *blocklist.Matcher
	SecurityDir       string
}

// Server represents the HTTP server
type Server struct {
	config               *Config
	runtimeConfig        atomic.Pointer[config.Config]
	router               *chi.Mux
	server               *http.Server
	baseHandler          atomic.Value
	limiter              *rateLimiter
	reportLimiter        *reportRateLimiter
	strictTokenLimiter   *rateLimiter
	metrics              *metrics.Metrics
	graphQL              map[string]*graphapi.Schema
	graphQLMu            sync.Mutex
	trustedProxies       []*net.IPNet
	trustedMu            sync.RWMutex
	trustedProxyStop     chan struct{}
	trustedProxyStopOnce sync.Once
	torBackendMu         sync.Mutex
	torBackendListener   net.Listener
	torBackendPort       int
	torOnionAddress      atomic.Value
	geoip                *geoip.Manager
	blocklist            *blocklist.Matcher
	securityDir          string
	healthRegistry       *health.Registry
	healthCoreMu         sync.RWMutex
	healthCore           ChecksInfo
	healthCoreRefreshed  time.Time
	allowlist            []netip.Prefix
	blockedIPs           []netip.Prefix
	startTime            time.Time
	stats                *statsCollector
	contactMu            sync.Mutex
	contactChallenges    map[string]contactChallenge
	contactRequests      map[string][]time.Time
	contactTokens        map[string]time.Time
	contactReviews       map[string]time.Time
	learnedOriginsMu     sync.Mutex
	learnedOrigins       map[string]time.Time
}

// New creates a new server instance
func New(config *Config) *Server {
	s := &Server{
		config:             config,
		graphQL:            make(map[string]*graphapi.Schema),
		router:             chi.NewRouter(),
		geoip:              config.GeoIP,
		blocklist:          config.Blocklist,
		securityDir:        config.SecurityDir,
		healthRegistry:     health.NewRegistry(),
		startTime:          time.Now(),
		trustedProxyStop:   make(chan struct{}),
		reportLimiter:      newReportRateLimiter(),
		stats:              newStatsCollector(),
		strictTokenLimiter: newRateLimiter(5, 60),
		contactChallenges:  map[string]contactChallenge{},
		contactRequests:    map[string][]time.Time{},
		contactTokens:      map[string]time.Time{},
		contactReviews:     map[string]time.Time{},
		learnedOrigins:     map[string]time.Time{},
	}
	s.torOnionAddress.Store("")
	if config.Cfg != nil {
		s.runtimeConfig.Store(config.Cfg)
	}

	// Prometheus metrics subsystem (AI.md PART 20). Built on
	// prometheus/client_golang; disabled when the operator turns it off.
	if config.Cfg == nil || config.Cfg.Server.Metrics.Enabled {
		mOpts := metrics.Options{
			Version:         config.Version,
			Commit:          config.Commit,
			BuildDate:       config.BuildDate,
			IncludeRuntime:  true,
			IncludeSystem:   true,
			DatabaseStatsFn: db.ConnectionStats,
			OwnerTokenCountsFn: func() (int, int) {
				primary, linked, err := db.ActiveOwnerTokenCounts(time.Now())
				if err != nil {
					return 0, 0
				}
				return primary, linked
			},
		}
		if config.Templates != nil {
			templates := config.Templates
			mOpts.TemplatesFn = func() int { return templates.Count() }
		}
		if config.Cfg != nil {
			mOpts.IncludeRuntime = config.Cfg.Server.Metrics.IncludeRuntime
			mOpts.IncludeSystem = config.Cfg.Server.Metrics.IncludeSystem
			mOpts.DurationBuckets = config.Cfg.Server.Metrics.DurationBuckets
			mOpts.SizeBuckets = config.Cfg.Server.Metrics.SizeBuckets
		}
		if config.Paths != nil {
			dataDir := config.Paths.GetDataDir()
			mOpts.SystemFn = func() metrics.SystemSnapshot { return systemMetricsSnapshot(dataDir) }
		}
		s.metrics = metrics.New(mOpts)
		db.SetQueryObserver(func(operation, table string, duration time.Duration, err error) {
			s.metrics.DBQueriesTotal.WithLabelValues(operation, table).Inc()
			s.metrics.DBQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
			if err != nil {
				s.metrics.DBErrorsTotal.WithLabelValues(operation, db.QueryErrorType(err)).Inc()
			}
		})
	} else {
		db.SetQueryObserver(nil)
	}

	// Enable per-IP rate limiting only when the operator turns it on.
	if config.Cfg != nil && config.Cfg.Server.RateLimit.Enabled {
		s.limiter = newRateLimiter(config.Cfg.Server.RateLimit.Requests, config.Cfg.Server.RateLimit.Window)
	}

	// Parse the trusted-proxy allowlist once at startup (AI.md PART 12).
	var additional []string
	if config.Cfg != nil {
		additional = config.Cfg.Server.TrustedProxies.Additional
		for _, entry := range config.Cfg.Server.Security.Allowlist {
			if prefix, ok := parseSecurityPrefix(entry.CIDR); ok && !overlyBroadSecurityPrefix(prefix) {
				s.allowlist = append(s.allowlist, prefix)
			} else {
				log.Printf("config warning: server.security.allowlist entry has invalid or overly broad cidr and was skipped")
			}
		}
		for _, entry := range config.Cfg.Server.Security.BlockedIPs {
			if prefix, ok := parseSecurityPrefix(entry.CIDR); ok {
				s.blockedIPs = append(s.blockedIPs, prefix)
			} else {
				log.Printf("config warning: server.security.blocked_ips entry has invalid cidr and was skipped")
			}
		}
	}
	s.trustedProxies = buildTrustedProxies(config.Address, additional)
	s.registerHealthChecks()

	// Apply the configured language-cookie name and lifetime (AI.md PART 30).
	if config.Cfg != nil {
		i18n.Configure(config.Cfg.Server.I18n.CookieName, parseCookieMaxAge(config.Cfg.Server.I18n.CookieMaxAge))
	}

	s.setupMiddleware()
	s.setupRoutes()

	// Mount the router under the configured URL path prefix (AI.md PART 8
	// "--baseurl"). The default "/" leaves routing untouched; a non-root prefix
	// strips the prefix before dispatch and redirects the bare prefix to its
	// trailing-slash form.
	var handler http.Handler = s.router
	if config.Cfg != nil {
		if prefix := config.Cfg.Server.BaseURL; prefix != "" && prefix != "/" {
			handler = baseURLHandler(prefix, s.router)
		}
	}
	s.baseHandler.Store(handler)

	s.server = &http.Server{
		Addr: fmt.Sprintf("%s:%d", config.Address, config.Port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.isTorRequest(r) {
				r = s.markTorRequest(r)
				if s.metrics != nil {
					s.metrics.TorRequestsTotal.Inc()
				}
			}
			current := s.baseHandler.Load().(http.Handler)
			current.ServeHTTP(w, r)
		}),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) currentConfig() *config.Config {
	if s == nil {
		return nil
	}
	if cfg := s.runtimeConfig.Load(); cfg != nil {
		return cfg
	}
	if s.config != nil {
		return s.config.Cfg
	}
	return nil
}

// ReplaceConfig atomically publishes a file-reloaded configuration snapshot.
func (s *Server) ReplaceConfig(cfg *config.Config) {
	if cfg != nil {
		s.runtimeConfig.Store(cfg)
		if s.server != nil {
			var handler http.Handler = s.router
			if prefix := cfg.Server.BaseURL; prefix != "" && prefix != "/" {
				handler = baseURLHandler(prefix, s.router)
			}
			s.baseHandler.Store(handler)
		}
	}
}

// baseURLHandler serves next under a non-root URL path prefix: the bare prefix
// is redirected to its trailing-slash form and all other requests have the
// prefix stripped before dispatch (AI.md PART 8 "--baseurl").
func baseURLHandler(prefix string, next http.Handler) http.Handler {
	stripped := http.StripPrefix(prefix, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix {
			http.Redirect(w, r, prefix+"/", http.StatusMovedPermanently)
			return
		}
		stripped.ServeHTTP(w, r)
	})
}

// setupMiddleware configures middleware
func (s *Server) setupMiddleware() {
	// Basic middleware. chi's middleware.RealIP is deliberately NOT used: it
	// rewrites r.RemoteAddr from X-Forwarded-For/X-Real-IP unconditionally,
	// which lets any direct client spoof its IP. Client IP is resolved through
	// the trusted-proxy gate in s.clientIP (AI.md PART 12).
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	// Security headers on every response (AI.md PART 11)
	s.router.Use(s.securityHeaders)
	s.router.Use(s.ownerCookieMiddleware)
	s.router.Use(s.allowlistMiddleware)
	s.router.Use(s.blocklistMiddleware)

	// Per-IP rate limiting (no-op unless enabled in config)
	s.router.Use(s.rateLimitMiddleware)

	// Country-based access policy (AI.md PART 19). Runs AFTER rate limiting and
	// never replaces authentication — a risk signal only, fail-open on any
	// lookup gap. No-op when GeoIP is disabled or no country database is loaded.
	s.router.Use(s.geoipMiddleware)

	// Record HTTP request metrics (AI.md PART 20)
	s.router.Use(s.metricsMiddleware)

	// Resolve and persist the request language (AI.md PART 30). No-op for
	// behavior when i18n is disabled — it still resolves to English.
	if s.currentConfig() == nil || s.currentConfig().Server.I18n.Enabled {
		s.router.Use(func(next http.Handler) http.Handler {
			return i18n.MiddlewareWithSecure(next, func(r *http.Request) bool { return preferenceCookieSecure(s, r) })
		})
	}

	// Timeout
	s.router.Use(middleware.Timeout(30 * time.Second))

	// Compression
	s.router.Use(middleware.Compress(5))

	s.router.Use(s.corsMiddleware)
}

// setupRoutes configures all routes
func (s *Server) setupRoutes() {
	// Themed error handlers for unmatched routes and methods (AI.md PART 16)
	s.router.NotFound(s.handleNotFound)
	s.router.MethodNotAllowed(s.handleMethodNotAllowed)

	// Public routes
	s.router.Get("/", s.handleHome)

	// Optional root alias for /server/healthz — only when the operator enables
	// it (AI.md PART 13 "Optional /healthz"). It mounts the same handler, so it
	// follows the exact same content negotiation as /server/healthz.
	if s.currentConfig() != nil && s.currentConfig().Server.Healthz.Root.Enabled {
		s.router.Get("/healthz", s.handleHealthz)
	}

	// Internal monitoring services use per-service bearer authentication. Every
	// alias mounts the same handler because scrapers do not follow redirects.
	if s.metrics != nil && s.currentConfig() != nil && s.currentConfig().Server.Metrics.Enabled {
		for _, service := range []string{"prometheus", "grafana", "loki"} {
			handler := s.metricsServiceHandler(service)
			canonical := "/server/metrics/" + service
			s.router.Method(http.MethodGet, canonical, handler)
			s.router.Method(http.MethodGet, s.apiBasePath()+canonical, handler)
			s.router.Method(http.MethodGet, "/api/metrics/"+service, handler)
			if s.currentConfig() == nil || s.currentConfig().Server.Metrics.Root.Enabled {
				s.router.Method(http.MethodGet, "/metrics/"+service, handler)
			}
			if service == "prometheus" {
				s.router.Method(http.MethodGet, "/server/metrics", handler)
				s.router.Method(http.MethodGet, s.apiBasePath()+"/server/metrics", handler)
				s.router.Method(http.MethodGet, "/api/metrics", handler)
				if s.currentConfig() == nil || s.currentConfig().Server.Metrics.Root.Enabled {
					s.router.Method(http.MethodGet, "/metrics", handler)
				}
			}
		}
	}

	// Special files (PWA, robots, security)
	s.router.Get("/robots.txt", s.handleRobotsTxt)
	s.router.Get("/security.txt", s.handleSecurityTxt)
	s.router.Get("/.well-known/security.txt", s.handleSecurityTxt)
	s.router.Get("/.well-known/llms.txt", s.handleLLMSTxt)
	s.router.Head("/.well-known/llms.txt", s.handleLLMSTxt)
	s.router.Get("/llms.txt", s.handleLLMSTxt)
	s.router.Head("/llms.txt", s.handleLLMSTxt)
	s.router.Get("/.well-known/pgp-key.asc", s.handlePGPKey)
	s.router.Get("/manifest.json", s.handleManifest)
	s.router.Get("/sw.js", s.handleServiceWorker)

	// Frontend locale catalogs (AI.md PART 30 "/locales/{lang}.json")
	s.router.Get("/locales/{lang}.json", s.handleLocaleJSON)

	// Search
	s.router.Get("/search", s.handleSearchPage)

	// Template detail
	s.router.Get("/template/{name}", s.handleTemplatePage)

	// Combine
	s.router.Get("/combine", s.handleCombinePage)

	// Categories
	s.router.Get("/categories", s.handleCategoriesPage)

	// List
	s.router.Get("/list", s.handleListPage)

	// Stats
	s.router.Get("/stats", s.handleStatsPage)

	// Docs
	s.router.Get("/docs", s.handleDocsPage)

	// CLI
	s.router.Get("/cli", s.handleCLIPage)

	// Standard public pages under /server/* (IDEA.md "Business logic",
	// AI.md PART 16 "Standard pages"). No admin UI exists here.
	s.router.Get("/server", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/server/about", http.StatusMovedPermanently)
	})

	// Canonical frontend health endpoint (AI.md PART 13). Content-negotiated
	// via PART 14 rules by the shared handler.
	s.router.Get("/server/healthz", s.handleHealthz)
	s.router.Get("/server/about", s.handleAboutPage)
	s.router.Get("/server/privacy", s.handlePrivacyPage)
	s.router.Get("/server/dpo", s.handleDPOPage)
	s.router.Get("/server/contact", s.handleContactPage)
	s.router.Post("/server/contact", s.handleContactSubmit)
	s.router.Get("/server/help", s.handleHelpPage)
	s.router.Get("/server/help/mcp", s.handleMCPHelpPage)
	s.router.Get("/server/preferences", s.handlePreferencesPage)
	s.router.Post("/server/preferences", s.handlePreferencesSave)
	s.router.Get("/server/preferences/tokens", s.handleOwnerTokensPage)
	s.router.Post("/server/preferences/token/create", s.handleIssueOwnerTokenWeb)
	s.router.Post("/server/preferences/token/restore", s.handleRestoreOwnerToken)
	s.router.Post("/server/preferences/token/forget", s.handleForgetOwnerToken)
	s.router.Post("/server/preferences/token/reveal", s.handleOwnerTokenWebAction)
	s.router.Post("/server/preferences/token/rotate", s.handleOwnerTokenWebAction)
	s.router.Post("/server/preferences/tokens", s.handleCreateLinkedOwnerTokenWeb)
	s.router.Post("/server/preferences/tokens/{id}/reveal", s.handleOwnerTokenWebAction)
	s.router.Post("/server/preferences/tokens/{id}/rotate", s.handleOwnerTokenWebAction)
	s.router.Post("/server/preferences/tokens/{id}/update", s.handleOwnerTokenWebAction)
	s.router.Post("/server/preferences/tokens/{id}/revoke", s.handleOwnerTokenWebAction)
	s.router.Get("/server/preferences/sync", s.handlePreferencesSyncPage)
	s.router.Post("/server/preferences/sync", s.handlePreferencesSyncSubmit)
	s.router.Get("/server/preferences/sync/import", s.handlePreferencesSyncImport)
	s.router.Get("/server/terms", s.handleTermsPage)
	s.router.Get("/server/security", s.handleSecurityPage)
	s.router.Get("/server/security/policy", s.handleSecurityPolicyPage)
	s.router.Get("/server/security/thanks", s.handleSecurityThanksPage)
	s.router.Get("/server/security/report/{trackingID}", s.handleSecurityReportStatus)
	s.router.Get("/server/tracking.js", s.handleTrackingJS)
	s.router.Post("/server/consent", s.handleConsent)
	s.router.Post("/server/ccpa", s.handleCCPA)

	// No-JS theme switch endpoint, also used by the shared preference control.
	s.router.Post("/server/theme", s.handleThemeSet)

	// Server docs UI pages (root level, AI.md PART 14 "Root-Level Endpoints")
	s.router.Get("/server/docs/swagger", s.handleSwaggerUI)
	s.router.Get("/server/docs/graphql", s.handleGraphiQLPage)

	// Static files
	s.router.Get("/static/*", s.handleStatic)
	s.router.Get("/favicon.ico", s.handleFavicon)

	// Root-level API aliases (AI.md PART 14 "Root-Level Endpoints") — thin
	// wrappers over the canonical versioned handlers, no logic duplication.
	s.router.Get("/api/swagger", s.handleOpenAPIJSON)
	s.router.Post("/api/graphql", s.handleGraphQL)
	s.router.Handle("/api/mcp", http.HandlerFunc(s.handleMCP))
	s.router.Handle("/mcp", http.HandlerFunc(s.handleMCP))
	s.router.Get("/api/healthz", s.handleHealthz)
	s.router.Get("/api/healthz.txt", s.handleHealthz)
	s.router.Get("/api/autodiscover", s.handleAPIAutodiscover)

	// Versioned API routes
	s.router.Route(s.apiBasePath(), func(r chi.Router) {
		// API info
		r.Get("/", s.handleAPIInfo)

		// Public server information endpoints.
		r.Get("/server/healthz", s.handleHealthz)
		r.Get("/server/healthz.txt", s.handleHealthz)
		r.Get("/server/swagger", s.handleOpenAPIJSON)
		r.Post("/server/reports/csp", s.handleCSPReport)
		r.Post("/server/reports/default", s.handleDefaultReport)
		r.Post("/server/reports/nel", s.handleNELReport)
		r.Post("/server/graphql", s.handleGraphQL)
		r.Handle("/server/mcp", http.HandlerFunc(s.handleMCP))

		// Read-only scheduler status (AI.md PART 18 "Scheduler Status")
		r.Get("/server/scheduler", s.handleAPISchedulerStatus)
		r.Get("/server/preferences", s.handleAPIPreferences)
		r.Post("/server/preferences", s.handleAPIPreferencesSave)
		r.Get("/server/preferences/tokens", s.handleAPIOwnerTokens)
		r.Post("/server/preferences/tokens", s.handleAPIOwnerTokens)
		r.Post("/server/preferences/tokens/primary", s.handleAPIPrimaryOwnerToken)
		r.Post("/server/preferences/tokens/primary/reveal", s.handleAPIPrimaryTokenAction)
		r.Post("/server/preferences/tokens/primary/rotate", s.handleAPIPrimaryTokenAction)
		r.Post("/server/preferences/tokens/{id}/reveal", s.handleAPILinkedTokenAction)
		r.Post("/server/preferences/tokens/{id}/rotate", s.handleAPILinkedTokenAction)
		r.Patch("/server/preferences/tokens/{id}", s.handleAPILinkedTokenAction)
		r.Delete("/server/preferences/tokens/{id}", s.handleAPILinkedTokenAction)
		r.Get("/server/preferences/sync", s.handleAPIPreferencesSync)
		r.Get("/server/preferences/sync/import", s.handleAPIPreferencesSyncImport)
		r.Get("/server/about", s.handleAPIAbout)
		r.Get("/server/privacy", s.handleAPIPrivacy)
		r.Get("/server/help", s.handleAPIHelp)
		r.Get("/server/terms", s.handleAPITerms)
		r.Get("/server/help/mcp", s.handleAPIHelpMCP)
		r.Get("/server/security", s.handleAPISecurity)
		r.Get("/server/security/policy", s.handleAPISecurityPolicy)
		r.Get("/server/security/thanks", s.handleAPISecurityThanks)
		r.Get("/server/dpo", s.handleAPIDPO)
		r.Get("/server/contact", s.handleAPIContact)
		r.Post("/server/contact", s.handleContactSubmit)

		// Templates (plural resource noun, AI.md PART 14 "Route Naming
		// Convention")
		r.Get("/templates/{name}", s.handleAPITemplate)
		r.Get("/templates/{name}.txt", s.handleAPITemplateText)
		r.Get("/templates/{name}.json", s.handleAPITemplateJSON)
		r.Get("/list", s.handleAPIList)
		r.Get("/list.txt", s.handleAPIListText)
		r.Get("/search", s.handleAPISearch)
		r.Get("/search.txt", s.handleAPISearchText)
		r.Get("/combine", s.handleAPICombine)
		r.Get("/combine.txt", s.handleAPICombineText)
		r.Get("/categories", s.handleAPICategories)
		r.Get("/categories.txt", s.handleAPICategoriesText)
		r.Get("/categories/{name}", s.handleAPICategoryTemplates)
		r.Get("/categories/{name}.txt", s.handleAPICategoryTemplatesText)
		r.Get("/stats", s.handleAPIStats)
		r.Get("/stats.txt", s.handleAPIStatsText)

		// Export
		r.Get("/templates.json", s.handleAPITemplatesJSON)
		r.Get("/templates.tar.gz", s.handleAPITemplatesTarGz)

		// CLI scripts
		r.Get("/cli/sh", s.handleCLIScriptSh)
		r.Get("/cli/ps", s.handleCLIScriptPs)
		r.Get("/cli/completion/bash", s.handleCLICompletionBash)
		r.Get("/cli/completion/zsh", s.handleCLICompletionZsh)
		r.Get("/cli/completion/fish", s.handleCLICompletionFish)
	})

	// gitignore.io route/API compatibility layer (unversioned, mounted
	// alongside the versioned API — see IDEA.md "External API
	// Compatibility")
	s.router.Get("/api/list", s.handleCompatList)
	s.router.Get("/api/{list}", s.handleCompatTemplates)

	// Debug routes (custom endpoints, net/http/pprof profiles and the expvar
	// /debug/vars handler) are gated on the independent debug flag (--debug /
	// DEBUG=true), never on application mode (AI.md PART 6). See debug_pprof.go.
	if mode.ShouldShowDebugEndpoints() {
		s.registerDebugRoutes(s.router)
	}
}

// Start binds the listener, drops root privileges to the configured
// service user/group (Unix only — see AI.md PART 23), then serves.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to bind %s: %w", s.server.Addr, err)
	}
	go s.refreshTrustedProxiesLoop()
	defer s.stopTrustedProxyRefresh()

	// Once the privileged port is bound, drop from root to the configured
	// service user/group. No-op if not running as root, and no-op on
	// Windows (which runs as a Virtual Service Account instead).
	user, group := "gitignore", "gitignore"
	if s.currentConfig() != nil {
		if s.currentConfig().Server.User != "" {
			user = s.currentConfig().Server.User
		}
		if s.currentConfig().Server.Group != "" {
			group = s.currentConfig().Server.Group
		}
	}
	if err := dropPrivileges(user, group); err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to drop privileges: %w", err)
	}

	// Wire TLS when SSL is enabled (AI.md PART 15). Certificates come from an
	// existing certbot/manual location or Let's Encrypt via autocert.
	if s.currentConfig() != nil && s.currentConfig().Server.SSL.Enabled {
		if err := s.configureTLS(); err != nil {
			_ = listener.Close()
			return fmt.Errorf("failed to configure TLS: %w", err)
		}
	}

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	// Start server
	log.Printf("Server starting on %s", s.server.Addr)
	if s.server.TLSConfig != nil {
		if err := s.server.ServeTLS(listener, "", ""); err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
	} else if err := s.server.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	log.Println("Server stopped")
	s.stopTrustedProxyRefresh()
	return nil
}

// configureTLS builds the server's TLS configuration from the SSL manager,
// resolving certificates for the configured FQDN (AI.md PART 15).
func (s *Server) configureTLS() error {
	certPath := "ssl"
	if s.config.Paths != nil {
		certPath = s.config.Paths.DataPath("ssl")
	}

	mgr := ssl.NewManager(ssl.Config{
		Enabled:  true,
		CertPath: certPath,
		LetsEncrypt: ssl.LetsEncryptConfig{
			Enabled:     s.currentConfig().Server.SSL.LetsEncrypt.Enabled,
			Email:       s.currentConfig().Server.SSL.LetsEncrypt.Email,
			Challenge:   s.currentConfig().Server.SSL.LetsEncrypt.Challenge,
			DNSProvider: s.currentConfig().Server.SSL.LetsEncrypt.DNSProvider,
		},
	})

	var domains []string
	if s.currentConfig().Server.FQDN != "" {
		domains = append(domains, s.currentConfig().Server.FQDN)
	}

	tlsConfig, err := mgr.GetTLSConfig(domains)
	if err != nil {
		return err
	}
	s.server.TLSConfig = tlsConfig
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopTrustedProxyRefresh()
	_ = s.SetTorBackendPort(0)
	return s.server.Shutdown(ctx)
}

// handleLocaleJSON serves the raw embedded translation catalog for a language
// so the frontend can fetch strings at runtime (AI.md PART 30). Unsupported
// languages fall back to the default locale rather than 404, so the client
// always receives a usable catalog.
func (s *Server) handleLocaleJSON(w http.ResponseWriter, r *http.Request) {
	lang := chi.URLParam(r, "lang")
	data, ok := i18n.LocaleJSON(lang)
	if !ok {
		data, _ = i18n.LocaleJSON(i18n.DefaultLang)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

// parseCookieMaxAge converts a config duration such as "365d", "24h", or
// "3600s" into whole seconds for the language cookie. Unrecognized or empty
// values fall back to one year, matching the AI.md PART 30 default.
func parseCookieMaxAge(s string) int {
	const oneYear = 365 * 24 * 60 * 60
	s = strings.TrimSpace(s)
	if s == "" {
		return oneYear
	}
	unit := s[len(s)-1]
	if unit >= '0' && unit <= '9' {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
		return oneYear
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return oneYear
	}
	switch unit {
	case 'd':
		return n * 24 * 60 * 60
	case 'h':
		return n * 60 * 60
	case 'm':
		return n * 60
	case 's':
		return n
	case 'y':
		return n * 365 * 24 * 60 * 60
	default:
		return oneYear
	}
}

// detectServerURL determines the server URL from request headers
func (s *Server) detectServerURL(r *http.Request) string {
	if s.isTorRequest(r) {
		address, _ := s.torOnionAddress.Load().(string)
		if address == "" && s.currentConfig() != nil {
			address = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s.currentConfig().Tor.OnionAddress)), ".onion")
		}
		if address != "" {
			return "http://" + address + ".onion"
		}
	}
	if host, overlay := overlayRequestHost(r.Host); overlay {
		return "http://" + host
	}
	// Reverse-proxy headers are only honored when the immediate peer is a
	// trusted proxy (AI.md PART 12 "X-Forwarded-* trust gate"). Headers from a
	// non-trusted peer are dropped so an attacker reaching the binary directly
	// cannot forge the proto/host used for URL construction.
	if s.isTrustedPeer(r.RemoteAddr) {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			host := r.Header.Get("X-Forwarded-Host")
			if host == "" {
				host = r.Host
			}
			return fmt.Sprintf("%s://%s", proto, host)
		}
	}

	// Check for config FQDN
	if s.currentConfig() != nil && s.currentConfig().Server.FQDN != "" {
		if host, overlay := overlayRequestHost(s.currentConfig().Server.FQDN); overlay {
			return "http://" + host
		}
		return fmt.Sprintf("https://%s", s.currentConfig().Server.FQDN)
	}

	// Default to request host
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func overlayRequestHost(host string) (string, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", false
	}
	name := host
	port := ""
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		name, port = parsedHost, parsedPort
	}
	lower := strings.ToLower(strings.TrimSuffix(name, "."))
	if !strings.HasSuffix(lower, ".i2p") && !strings.HasSuffix(lower, ".exit") {
		return "", false
	}
	if port == "80" {
		return name, true
	}
	if port != "" {
		return net.JoinHostPort(name, port), true
	}
	return name, true
}
