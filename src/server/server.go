package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apimgr/gitignore/src/admin"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/geoip"
	"github.com/apimgr/gitignore/src/mode"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/server/metrics"
	"github.com/apimgr/gitignore/src/ssl"
	"github.com/apimgr/gitignore/src/template"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"
)

// apiVersion is the current API version segment. Code must build API paths
// through apiBasePath() rather than hardcoding "v1" (AI.md PART 14).
const apiVersion = "v1"

// apiBasePath returns the versioned API path prefix, e.g. "/api/v1".
func apiBasePath() string {
	return "/api/" + apiVersion
}

// Config holds server configuration
type Config struct {
	Address   string
	Port      int
	DevMode   bool
	Templates *template.Manager
	Paths     *apppath.PathManager
	Version   string
	Commit    string
	BuildDate string
	Cfg       *config.Config
	GeoIP     *geoip.Manager
}

// Server represents the HTTP server
type Server struct {
	config        *Config
	router        *chi.Mux
	server        *http.Server
	adminHandler  *admin.Handler
	limiter       *rateLimiter
	metrics       *metrics.Metrics
	trustedProxies []*net.IPNet
	geoip         *geoip.Manager
	startTime     time.Time
	stats         *statsCollector
}

// New creates a new server instance
func New(config *Config) *Server {
	s := &Server{
		config:    config,
		router:    chi.NewRouter(),
		geoip:     config.GeoIP,
		startTime: time.Now(),
		stats:     newStatsCollector(),
	}

	// Prometheus metrics subsystem (AI.md PART 20). Built on
	// prometheus/client_golang; disabled when the operator turns it off.
	if config.Cfg == nil || config.Cfg.Server.Metrics.Enabled {
		mOpts := metrics.Options{
			Version:        config.Version,
			Commit:         config.Commit,
			BuildDate:      config.BuildDate,
			IncludeRuntime: true,
		}
		if config.Templates != nil {
			templates := config.Templates
			mOpts.TemplatesFn = func() int { return templates.Count() }
		}
		if config.Cfg != nil {
			mOpts.IncludeRuntime = config.Cfg.Server.Metrics.IncludeRuntime
			mOpts.DurationBuckets = config.Cfg.Server.Metrics.DurationBuckets
			mOpts.SizeBuckets = config.Cfg.Server.Metrics.SizeBuckets
		}
		s.metrics = metrics.New(mOpts)
	}

	// Enable per-IP rate limiting only when the operator turns it on.
	if config.Cfg != nil && config.Cfg.Server.RateLimit.Enabled {
		s.limiter = newRateLimiter(config.Cfg.Server.RateLimit.Requests, config.Cfg.Server.RateLimit.Window)
	}

	// Parse the trusted-proxy allowlist once at startup (AI.md PART 12).
	var additional []string
	if config.Cfg != nil {
		additional = config.Cfg.Server.TrustedProxies.Additional
	}
	s.trustedProxies = buildTrustedProxies(config.Address, additional)

	// Load admin credentials from database (never from config file)
	adminUsername := "admin"
	adminPassHash := ""
	adminTokenHash := ""
	if creds, err := db.GetAdminCredentials(); err == nil && creds != nil {
		adminUsername = creds.Username
		adminPassHash = creds.PassHash
		adminTokenHash = creds.TokenHash
	}

	sslEnabled := config.Cfg != nil && config.Cfg.Server.SSL.Enabled
	s.adminHandler = admin.NewHandler(
		adminUsername,
		adminPassHash,
		adminTokenHash,
		3600,
		sslEnabled,
		config.Version,
		config.Commit,
		config.BuildDate,
	)

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

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Address, config.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
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
	if s.config.Cfg == nil || s.config.Cfg.Server.I18n.Enabled {
		s.router.Use(i18n.Middleware)
	}

	// Timeout
	s.router.Use(middleware.Timeout(30 * time.Second))

	// Compression
	s.router.Use(middleware.Compress(5))

	// CORS
	corsOrigin := "*"
	if s.config.Cfg != nil && s.config.Cfg.WebSecurity.CORS != "" {
		corsOrigin = s.config.Cfg.WebSecurity.CORS
	}
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{corsOrigin},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders: []string{"Link"},
		MaxAge:         300,
	})
	s.router.Use(corsHandler.Handler)
}

// setupRoutes configures all routes
func (s *Server) setupRoutes() {
	// Admin routes (session auth for web, bearer token for API)
	s.adminHandler.RegisterRoutes(s.router)

	// Themed error handlers for unmatched routes and methods (AI.md PART 16)
	s.router.NotFound(s.handleNotFound)
	s.router.MethodNotAllowed(s.handleMethodNotAllowed)

	// Public routes
	s.router.Get("/", s.handleHome)

	// Optional root alias for /server/healthz — only when the operator enables
	// it (AI.md PART 13 "Optional /healthz"). It mounts the same handler, so it
	// follows the exact same content negotiation as /server/healthz.
	if s.config.Cfg != nil && s.config.Cfg.Server.Healthz.Root.Enabled {
		s.router.Get("/healthz", s.handleHealthz)
	}

	// Prometheus metrics (internal only — firewall externally, AI.md PART 20).
	// Registered only when metrics are enabled; the endpoint path and optional
	// bearer token come from server.metrics config.
	if s.metrics != nil {
		endpoint := "/metrics"
		if s.config.Cfg != nil && s.config.Cfg.Server.Metrics.Endpoint != "" {
			endpoint = s.config.Cfg.Server.Metrics.Endpoint
		}
		s.router.Handle(endpoint, s.metricsHandler())
	}

	// Special files (PWA, robots, security)
	s.router.Get("/robots.txt", s.handleRobotsTxt)
	s.router.Get("/security.txt", s.handleSecurityTxt)
	s.router.Get("/.well-known/security.txt", s.handleSecurityTxt)
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
	s.router.Get("/server", s.handleServerPage)

	// Canonical frontend health endpoint (AI.md PART 13). Content-negotiated
	// via PART 14 rules by the shared handler.
	s.router.Get("/server/healthz", s.handleHealthz)
	s.router.Get("/server/about", s.handleAboutPage)
	s.router.Get("/server/privacy", s.handlePrivacyPage)
	s.router.Get("/server/contact", s.handleContactPage)
	s.router.Get("/server/help", s.handleHelpPage)
	s.router.Get("/server/terms", s.handleTermsPage)

	// No-JS theme switch endpoint for the <noscript> form (AI.md PART 16)
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
	s.router.Get("/api/graphql", s.handleGraphQLSchema)
	s.router.Post("/api/graphql", s.handleGraphQL)
	s.router.Get("/api/healthz", s.handleHealthz)
	s.router.Get("/api/healthz.txt", s.handleHealthz)
	s.router.Get("/api/autodiscover", s.handleAPIAutodiscover)

	// Versioned API routes
	s.router.Route(apiBasePath(), func(r chi.Router) {
		// API info
		r.Get("/", s.handleAPIInfo)

		// Operator/server namespace (AI.md PART 14 "server/*", info-only —
		// mutating operator endpoints are a separate follow-up, see
		// TODO.AI.md)
		r.Get("/server/healthz", s.handleHealthz)
		r.Get("/server/healthz.txt", s.handleHealthz)
		r.Get("/server/swagger", s.handleOpenAPIJSON)
		r.Post("/server/graphql", s.handleGraphQL)
		r.Get("/server/graphql", s.handleGraphQLSchema)

		// Read-only scheduler status (AI.md PART 18 "Scheduler Status")
		r.Get("/server/scheduler", s.handleAPISchedulerStatus)

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

	// Once the privileged port is bound, drop from root to the configured
	// service user/group. No-op if not running as root, and no-op on
	// Windows (which runs as a Virtual Service Account instead).
	user, group := "gitignore", "gitignore"
	if s.config.Cfg != nil {
		if s.config.Cfg.Server.User != "" {
			user = s.config.Cfg.Server.User
		}
		if s.config.Cfg.Server.Group != "" {
			group = s.config.Cfg.Server.Group
		}
	}
	if err := dropPrivileges(user, group); err != nil {
		listener.Close()
		return fmt.Errorf("failed to drop privileges: %w", err)
	}

	// Wire TLS when SSL is enabled (AI.md PART 15). Certificates come from an
	// existing certbot/manual location or Let's Encrypt via autocert.
	if s.config.Cfg != nil && s.config.Cfg.Server.SSL.Enabled {
		if err := s.configureTLS(); err != nil {
			listener.Close()
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
			Enabled:     s.config.Cfg.Server.SSL.LetsEncrypt.Enabled,
			Email:       s.config.Cfg.Server.SSL.LetsEncrypt.Email,
			Challenge:   s.config.Cfg.Server.SSL.LetsEncrypt.Challenge,
			DNSProvider: s.config.Cfg.Server.SSL.LetsEncrypt.DNSProvider,
		},
	})

	var domains []string
	if s.config.Cfg.Server.FQDN != "" {
		domains = append(domains, s.config.Cfg.Server.FQDN)
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
	default:
		return oneYear
	}
}

// detectServerURL determines the server URL from request headers
func (s *Server) detectServerURL(r *http.Request) string {
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
	if s.config.Cfg != nil && s.config.Cfg.Server.FQDN != "" {
		return fmt.Sprintf("https://%s", s.config.Cfg.Server.FQDN)
	}

	// Default to request host
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
