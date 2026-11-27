package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	Address             string
	Port                int
	HTTPSPort           int
	DevMode             bool
	Templates           *template.Manager
	Paths               *apppath.PathManager
	Version             string
	Commit              string
	BuildDate           string
	Cfg                 *config.Config
	ConfigManager       *config.ConfigManager
	GeoIP               *geoip.Manager
	SendContact         func(to, subject, body string) error
	SendContactReply    func(to, replyTo, subject, body string) error
	WebhookDispatcher   *notify.Dispatcher
	MaintenanceState    *MaintenanceState
	I2PStatus           func() I2PInfo
	Blocklist           *blocklist.Matcher
	SecurityDir         string
	BeforePrivilegeDrop func() error
	BeforeServe         func()
	OnReady             func()
	AccessLog           func(AccessLogEntry)
}

// Server represents the HTTP server
type Server struct {
	config               *Config
	runtimeConfig        atomic.Pointer[config.Config]
	router               *chi.Mux
	server               *http.Server
	sslManager           *ssl.Manager
	baseHandler          atomic.Value
	limiter              *rateLimiter
	rateLimiters         map[string]*rateLimiter
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
	abuseMu              sync.Mutex
	abuseRequests        map[string][]abuseRequest
	abuseAlerted         map[string]time.Time
	abusePrunedAt        time.Time
	temporaryBlocks      map[string]db.IPBlock
	abuseAlertHandler    func(db.IPBlock)
	securityPolicyMu     sync.RWMutex
	blockedIPReasons     map[string]string
	allowlistSnapshot    map[string]string
	startTime            time.Time
	updateRestart        atomic.Bool
	stats                *statsCollector
	statsPersistenceMu   sync.Mutex
	statsPersistenceStop context.CancelFunc
	statsPersistenceDone chan struct{}
	contactMu            sync.Mutex
	contactChallenges    map[string]contactChallenge
	contactRequests      map[string][]time.Time
	contactTokens        map[string]time.Time
	contactReviews       map[string]time.Time
	learnedOriginsMu     sync.Mutex
	learnedOrigins       map[string]time.Time
}

type baseHandlerSnapshot struct {
	handler http.Handler
}

var ErrUpdateRestart = errors.New("staged update requires supervisor restart")

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
		strictTokenLimiter: newRateLimiterFor("strict", 10, 900),
		rateLimiters: map[string]*rateLimiter{
			"read":          newRateLimiterFor("read", 120, 60),
			"write":         newRateLimiterFor("write", 10, 60),
			"health":        newRateLimiterFor("health", 120, 60),
			"auth_failures": newRateLimiterFor("auth_failures", 5, 900),
		},
		contactChallenges: map[string]contactChallenge{},
		contactRequests:   map[string][]time.Time{},
		contactTokens:     map[string]time.Time{},
		contactReviews:    map[string]time.Time{},
		learnedOrigins:    map[string]time.Time{},
		abuseRequests:     map[string][]abuseRequest{},
		abuseAlerted:      map[string]time.Time{},
		temporaryBlocks:   map[string]db.IPBlock{},
		blockedIPReasons:  map[string]string{},
		allowlistSnapshot: map[string]string{},
	}
	s.torOnionAddress.Store("")
	if config.Cfg != nil {
		s.runtimeConfig.Store(config.Cfg)
	}
	db.SetDebugQueryObserver(func(query string, argCount int, rows int64, duration time.Duration, err error) {
		if !mode.IsDebugEnabled() {
			return
		}
		cfg := s.currentConfig()
		if cfg == nil || !cfg.Server.Debug.LogQueries {
			return
		}
		failed := err != nil
		slog.Debug("database query", "sql", sanitizeDebugSQL(query), "arg_count", argCount, "rows", rows, "duration_ms", duration.Milliseconds(), "failed", failed)
	})
	db.SetDebugCacheObserver(func(operation, key string, hit bool, duration time.Duration) {
		if !mode.IsDebugEnabled() {
			return
		}
		cfg := s.currentConfig()
		if cfg == nil || !cfg.Server.Debug.LogCache {
			return
		}
		slog.Debug("cache operation", "operation", operation, "key", key, "hit", hit, "duration_us", duration.Microseconds())
	})
	if blocks, err := db.LoadIPBlocks(time.Now().UTC()); err != nil {
		if !errors.Is(err, sql.ErrConnDone) {
			log.Printf("security: failed to load temporary IP blocks: %v", err)
		}
	} else {
		for _, block := range blocks {
			s.temporaryBlocks[block.IP] = block
		}
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
		db.SetCacheMetricsObserver(func(cache string, hit bool) {
			if cache != "remote_db" && cache != "rate_limit" {
				return
			}
			if hit {
				s.metrics.CacheHitsTotal.WithLabelValues(cache).Inc()
			} else {
				s.metrics.CacheMissesTotal.WithLabelValues(cache).Inc()
			}
		})
		db.SetCacheEvictionObserver(func(cache string, count uint64) {
			if count > 0 && (cache == "remote_db" || cache == "rate_limit") {
				s.metrics.CacheEvictionsTotal.WithLabelValues(cache).Add(float64(count))
			}
		})
		db.SetQueryObserver(func(operation, table string, duration time.Duration, err error) {
			s.metrics.DBQueriesTotal.WithLabelValues(operation, table).Inc()
			s.metrics.DBQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
			if err != nil {
				s.metrics.DBErrorsTotal.WithLabelValues(operation, db.QueryErrorType(err)).Inc()
			}
		})
		writeMetricsEnabled := db.IsSQLite()
		db.SetWriteObserver(func(wait time.Duration, busy bool) {
			if !writeMetricsEnabled {
				return
			}
			s.metrics.DBWriteWait.Observe(wait.Seconds())
			if busy {
				s.metrics.DBBusyTotal.Inc()
			}
		})
	} else {
		db.SetQueryObserver(nil)
		db.SetWriteObserver(nil)
		db.SetCacheMetricsObserver(nil)
		db.SetCacheEvictionObserver(nil)
	}

	if config.Cfg != nil {
		rates := config.Cfg.Server.RateLimit
		s.limiter = newRateLimiterFor("global", rates.GlobalBurst, 60)
		s.rateLimiters = map[string]*rateLimiter{
			"read":          newRateLimiterFor("read", rates.Read.Requests, rates.Read.Window),
			"write":         newRateLimiterFor("write", rates.Write.Requests, rates.Write.Window),
			"health":        newRateLimiterFor("health", rates.Health.Requests, rates.Health.Window),
			"auth_failures": newRateLimiterFor("auth_failures", rates.AuthFailures.Requests, rates.AuthFailures.Window),
		}
		s.strictTokenLimiter = newRateLimiterFor("strict", rates.Strict.Requests, rates.Strict.Window)
	}

	// Parse the trusted-proxy allowlist once at startup (AI.md PART 12).
	var additional []string
	if config.Cfg != nil {
		additional = config.Cfg.Server.TrustedProxies.Additional
		s.refreshSecurityPolicy(config.Cfg, false)
	}
	s.trustedProxies = buildTrustedProxies(config.Address, additional)
	s.registerHealthChecks()

	s.setupMiddleware()
	s.setupRoutes()

	// Mount the router under the configured URL path prefix (AI.md PART 8
	// "--baseurl"). The default "/" leaves routing untouched; a non-root prefix
	// strips the prefix before dispatch and redirects the bare prefix to its
	// trailing-slash form.
	s.baseHandler.Store(baseHandlerSnapshot{handler: s.buildBaseHandler(config.Cfg)})

	s.server = &http.Server{
		Addr: fmt.Sprintf("%s:%d", config.Address, config.Port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.isTorRequest(r) {
				r = s.markTorRequest(r)
				if s.metrics != nil {
					s.metrics.TorRequestsTotal.Inc()
				}
			}
			current := s.baseHandler.Load().(baseHandlerSnapshot).handler
			if s.sslManager != nil {
				current = s.sslManager.GetHTTPHandler(current)
			}
			current.ServeHTTP(w, r)
		}),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// SetAbuseAlertHandler registers the best-effort email callback for flood alerts.
func (s *Server) SetAbuseAlertHandler(handler func(db.IPBlock)) {
	if s == nil {
		return
	}
	s.abuseMu.Lock()
	s.abuseAlertHandler = handler
	s.abuseMu.Unlock()
}

// CleanupExpiredIPBlocks removes expired entries returned by the durable
// cleanup task without deleting a newer block for the same address.
func (s *Server) CleanupExpiredIPBlocks(expired []db.IPBlock) {
	if s == nil || len(expired) == 0 {
		return
	}
	s.abuseMu.Lock()
	defer s.abuseMu.Unlock()
	for _, block := range expired {
		current, ok := s.temporaryBlocks[block.IP]
		if ok && current.ExpiresAt.Equal(block.ExpiresAt) {
			delete(s.temporaryBlocks, block.IP)
		}
	}
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
		s.refreshSecurityPolicy(cfg, true)
		s.refreshRateLimiters(cfg)
		s.runtimeConfig.Store(cfg)
		if s.server != nil {
			s.baseHandler.Store(baseHandlerSnapshot{handler: s.buildBaseHandler(cfg)})
		}
	}
}

func (s *Server) buildBaseHandler(cfg *config.Config) http.Handler {
	var handler http.Handler = s.router
	if cfg != nil {
		if prefix := cfg.Server.BaseURL; prefix != "" && prefix != "/" {
			handler = baseURLHandler(prefix, s.router)
		}
	}
	handler = flashRedirectMiddleware(s, handler)
	handler = s.accessLogMiddleware(handler)
	if s.stats != nil {
		handler = s.stats.middleware(handler)
	}
	return handler
}

func (s *Server) refreshRateLimiters(cfg *config.Config) {
	if s == nil || cfg == nil {
		return
	}
	rates := cfg.Server.RateLimit
	if s.limiter == nil {
		s.limiter = newRateLimiterFor("global", rates.GlobalBurst, 60)
	} else {
		s.limiter.configure(rates.GlobalBurst, 60)
	}
	if s.rateLimiters == nil {
		s.rateLimiters = make(map[string]*rateLimiter)
	}
	configured := map[string]config.RateLimitRuleConfig{
		"read":          rates.Read,
		"write":         rates.Write,
		"health":        rates.Health,
		"auth_failures": rates.AuthFailures,
	}
	for name, rule := range configured {
		limiter := s.rateLimiters[name]
		if limiter == nil {
			s.rateLimiters[name] = newRateLimiterFor(name, rule.Requests, rule.Window)
			continue
		}
		limiter.configure(rule.Requests, rule.Window)
	}
	if s.strictTokenLimiter == nil {
		s.strictTokenLimiter = newRateLimiterFor("strict", rates.Strict.Requests, rates.Strict.Window)
	} else {
		s.strictTokenLimiter.configure(rates.Strict.Requests, rates.Strict.Window)
	}
}

func (s *Server) refreshSecurityPolicy(cfg *config.Config, reportChanges bool) {
	if s == nil || cfg == nil {
		return
	}
	nextAllowlist := make(map[string]string, len(cfg.Server.Security.Allowlist))
	allowPrefixes := make([]netip.Prefix, 0, len(cfg.Server.Security.Allowlist))
	for _, entry := range cfg.Server.Security.Allowlist {
		prefix, ok := parseSecurityPrefix(entry.CIDR)
		if !ok || overlyBroadSecurityPrefix(prefix) {
			continue
		}
		cidr := prefix.String()
		description := strings.TrimSpace(entry.Description)
		if description == "" {
			description = "(no description)"
		}
		nextAllowlist[cidr] = description
		allowPrefixes = append(allowPrefixes, prefix)
	}
	nextBlocked := make([]netip.Prefix, 0, len(cfg.Server.Security.BlockedIPs))
	nextReasons := make(map[string]string, len(cfg.Server.Security.BlockedIPs))
	for _, entry := range cfg.Server.Security.BlockedIPs {
		prefix, ok := parseSecurityPrefix(entry.CIDR)
		if !ok {
			continue
		}
		nextBlocked = append(nextBlocked, prefix)
		nextReasons[prefix.String()] = entry.Reason
	}
	s.securityPolicyMu.Lock()
	previous := s.allowlistSnapshot
	s.allowlist = allowPrefixes
	s.blockedIPs = nextBlocked
	s.blockedIPReasons = nextReasons
	s.allowlistSnapshot = nextAllowlist
	s.securityPolicyMu.Unlock()
	s.abuseMu.Lock()
	var newlyAllowlisted []string
	for ip := range s.temporaryBlocks {
		address, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		for _, prefix := range allowPrefixes {
			if prefix.Contains(address) {
				newlyAllowlisted = append(newlyAllowlisted, ip)
				break
			}
		}
	}
	s.abuseMu.Unlock()
	for _, ip := range newlyAllowlisted {
		s.releaseTemporaryBlock(ip, "allowlisted")
	}
	if !reportChanges {
		return
	}
	for cidr, description := range nextAllowlist {
		if _, exists := previous[cidr]; !exists {
			s.auditSecurityEvent("security.ip_allowlisted", "info", "success", map[string]any{"cidr": cidr, "description": description})
		}
	}
	for cidr := range previous {
		if _, exists := nextAllowlist[cidr]; !exists {
			s.auditSecurityEvent("security.ip_allowlist_removed", "info", "success", map[string]any{"cidr": cidr})
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
	s.router.Use(urlNormalizeMiddleware)
	s.router.Use(middleware.RequestID)
	s.router.Use(s.debugMiddleware)
	s.router.Use(pathSecurityMiddleware)
	s.router.Use(s.maintenanceMiddleware)
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

	// Internationalization is always enabled (AI.md PART 30).
	s.router.Use(func(next http.Handler) http.Handler {
		return i18n.MiddlewareWithSecureAndConsent(next,
			func(r *http.Request) bool { return preferenceCookieSecure(s, r) },
			func(r *http.Request) bool {
				consent, valid := readConsent(r)
				return valid && consent.Preferences
			},
		)
	})

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

	// Special files and the operator-provisioned CLI update distribution.
	s.router.Get("/cli/binaries/{filename}", s.handleCLIBinary)
	s.router.Get("/robots.txt", s.handleRobotsTxt)
	s.router.Get("/sitemap.xml", s.handleSitemap)
	s.router.Get("/sitemap-{part}.xml", s.handleSitemapPart)
	s.router.Head("/sitemap.xml", s.handleSitemap)
	s.router.Head("/sitemap-{part}.xml", s.handleSitemapPart)
	s.router.Get("/feed.xml", s.handleAtomFeed)
	s.router.Head("/feed.xml", s.handleAtomFeed)
	s.router.Get("/rss.xml", s.handleRSSFeed)
	s.router.Head("/rss.xml", s.handleRSSFeed)
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
	s.router.Post("/server/preferences/token/revoke", s.handleRevokePrimaryOwnerTokenWeb)
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
	s.router.Post("/announcements/dismiss", s.handleAnnouncementDismiss)

	// No-JS theme switch endpoint, also used by the shared preference control.
	s.router.Post("/server/theme", s.handleThemeSet)

	// Server docs UI pages (root level, AI.md PART 14 "Root-Level Endpoints")
	s.router.Get("/server/docs/swagger", s.handleSwaggerUI)
	s.router.Get("/server/docs/graphql", s.handleGraphiQLPage)

	// Static files
	s.router.Get("/static/*", s.handleStatic)
	s.router.Get("/favicon.ico", s.handleFavicon)
	s.router.Get("/assets/{name}", s.handleBrandingAsset)
	s.router.Get("/branding/cache/{name}", s.handleCachedBrandingAsset)

	// Root-level API aliases (AI.md PART 14 "Root-Level Endpoints") — thin
	// wrappers over the canonical versioned handlers, no logic duplication.
	s.router.Get("/api/swagger", s.handleOpenAPIJSON)
	s.router.Post("/api/graphql", s.handleGraphQL)
	s.router.Get("/api/graphql", s.handleGraphQL)
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
		r.Get("/server/maintenance", s.handleMaintenanceStatus)
		r.Get("/server/swagger", s.handleOpenAPIJSON)
		r.Post("/server/reports/csp", s.handleCSPReport)
		r.Post("/server/reports/default", s.handleDefaultReport)
		r.Post("/server/reports/nel", s.handleNELReport)
		r.Get("/server/graphql", s.handleGraphQL)
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
		r.Delete("/server/preferences/tokens/primary", s.handleAPIPrimaryOwnerRevoke)
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
	var httpsListener net.Listener
	if s.config.HTTPSPort > 0 {
		httpsAddr := net.JoinHostPort(strings.Trim(s.config.Address, "[]"), strconv.Itoa(s.config.HTTPSPort))
		httpsListener, err = net.Listen("tcp", httpsAddr)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("failed to bind HTTPS listener %s: %w", httpsAddr, err)
		}
	}
	go s.refreshTrustedProxiesLoop()
	defer s.stopTrustedProxyRefresh()
	if s.config.BeforePrivilegeDrop != nil {
		if err := s.config.BeforePrivilegeDrop(); err != nil {
			if httpsListener != nil {
				_ = httpsListener.Close()
			}
			_ = listener.Close()
			return fmt.Errorf("startup update check failed: %w", err)
		}
	}

	// Once the privileged port is bound, drop from root to the fixed project
	// service identity. No-op when not running as root.
	if err := dropPrivileges("gitignore", "gitignore"); err != nil {
		if httpsListener != nil {
			_ = httpsListener.Close()
		}
		_ = listener.Close()
		return fmt.Errorf("failed to drop privileges: %w", err)
	}

	// Wire TLS when SSL is enabled (AI.md PART 15). Certificates come from an
	// existing certbot/manual location or Let's Encrypt via autocert.
	if s.currentConfig() != nil && s.currentConfig().Server.SSL.Enabled {
		if err := s.configureTLS(); err != nil {
			if httpsListener != nil {
				_ = httpsListener.Close()
			}
			_ = listener.Close()
			return fmt.Errorf("failed to configure TLS: %w", err)
		}
	}
	if httpsListener != nil && s.server.TLSConfig == nil {
		_ = httpsListener.Close()
		_ = listener.Close()
		return errors.New("dual port configuration requires server.ssl.enabled")
	}
	if s.config.BeforeServe != nil {
		s.config.BeforeServe()
	}
	s.startStatsPersistence()
	defer func() { _ = s.stopStatsPersistence(context.Background()) }()

	// Start server
	slog.Info("Server ready", "address", s.server.Addr)
	if s.config.OnReady != nil {
		s.config.OnReady()
	}
	if httpsListener != nil {
		serveErr := make(chan error, 2)
		go func() { serveErr <- s.server.Serve(listener) }()
		go func() { serveErr <- s.server.ServeTLS(httpsListener, "", "") }()
		if err := <-serveErr; err != nil && err != http.ErrServerClosed {
			return err
		}
	} else if s.server.TLSConfig != nil {
		if err := s.server.ServeTLS(listener, "", ""); err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
	} else if err := s.server.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	s.stopTrustedProxyRefresh()
	if s.updateRestart.Load() {
		return ErrUpdateRestart
	}
	return nil
}

// RestartForUpdate gracefully stops the listener after a new release has been
// staged so the service manager can restart the process with its original
// elevated startup privileges.
func (s *Server) RestartForUpdate(ctx context.Context) error {
	s.updateRestart.Store(true)
	return s.Shutdown(ctx)
}

// configureTLS builds the server's TLS configuration from the SSL manager,
// resolving certificates for the configured FQDN (AI.md PART 15).
func (s *Server) configureTLS() error {
	letsencryptConfig := s.currentConfig().Server.SSL.LetsEncrypt
	if strings.EqualFold(letsencryptConfig.Challenge, "dns-01") {
		if err := reconcileDNSCredentialState(letsencryptConfig.DNSProvider, s.currentConfig().Server.Security.EncryptionKey, letsencryptConfig.DNSCredentials); err != nil {
			return err
		}
	}
	certPath := "ssl"
	if s.config.Paths != nil {
		certPath = filepath.Join(s.config.Paths.GetConfigDir(), "ssl")
	}

	mgr := ssl.NewManager(ssl.Config{
		Enabled:  true,
		CertPath: certPath,
		LetsEncrypt: ssl.LetsEncryptConfig{
			Enabled:        s.currentConfig().Server.SSL.LetsEncrypt.Enabled,
			Email:          s.currentConfig().Server.SSL.LetsEncrypt.Email,
			Challenge:      s.currentConfig().Server.SSL.LetsEncrypt.Challenge,
			DNSProvider:    s.currentConfig().Server.SSL.LetsEncrypt.DNSProvider,
			DNSCredentials: s.currentConfig().Server.SSL.LetsEncrypt.DNSCredentials,
			OnDNSValidated: func(validatedAt time.Time) error {
				return db.Write(context.Background(), func(tx *sql.Tx) error {
					_, err := tx.ExecContext(context.Background(), `INSERT INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, "dns_validated_at", validatedAt.Format(time.RFC3339))
					return err
				})
			},
		},
	})
	s.sslManager = mgr

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

// RenewTLS runs the daily certificate renewal check using the active SSL
// manager and the server's configured public hostname.
func (s *Server) RenewTLS(ctx context.Context) error {
	if s == nil || s.sslManager == nil {
		return errors.New("SSL manager is unavailable")
	}
	var domains []string
	if cfg := s.currentConfig(); cfg != nil && cfg.Server.FQDN != "" {
		domains = append(domains, cfg.Server.FQDN)
	}
	return s.sslManager.Renew(ctx, domains)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	db.SetDebugQueryObserver(nil)
	db.SetDebugCacheObserver(nil)
	s.stopTrustedProxyRefresh()
	_ = s.SetTorBackendPort(0)
	shutdownErr := s.server.Shutdown(ctx)
	statsErr := s.stopStatsPersistence(ctx)
	return errors.Join(shutdownErr, statsErr)
}

func (s *Server) startStatsPersistence() {
	s.statsPersistenceMu.Lock()
	defer s.statsPersistenceMu.Unlock()
	if s.statsPersistenceStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.statsPersistenceStop = cancel
	s.statsPersistenceDone = done
	go func() {
		defer close(done)
		s.stats.runPersistence(ctx)
	}()
}

func (s *Server) stopStatsPersistence(ctx context.Context) error {
	s.statsPersistenceMu.Lock()
	cancel := s.statsPersistenceStop
	done := s.statsPersistenceDone
	s.statsPersistenceStop = nil
	s.statsPersistenceDone = nil
	s.statsPersistenceMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
