package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apimgr/gitignore/src/admin"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	apppath "github.com/apimgr/gitignore/src/path"
	"github.com/apimgr/gitignore/src/ssl"
	"github.com/apimgr/gitignore/src/template"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"
)

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
}

// Server represents the HTTP server
type Server struct {
	config       *Config
	router       *chi.Mux
	server       *http.Server
	adminHandler *admin.Handler
	limiter      *rateLimiter
	metrics      *metricsRegistry
}

// New creates a new server instance
func New(config *Config) *Server {
	s := &Server{
		config:  config,
		router:  chi.NewRouter(),
		metrics: newMetricsRegistry(),
	}

	// Enable per-IP rate limiting only when the operator turns it on.
	if config.Cfg != nil && config.Cfg.Server.RateLimit.Enabled {
		s.limiter = newRateLimiter(config.Cfg.Server.RateLimit.Requests, config.Cfg.Server.RateLimit.Window)
	}

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

	s.setupMiddleware()
	s.setupRoutes()

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Address, config.Port),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// setupMiddleware configures middleware
func (s *Server) setupMiddleware() {
	// Basic middleware
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	// Security headers on every response (AI.md PART 11)
	s.router.Use(s.securityHeaders)

	// Per-IP rate limiting (no-op unless enabled in config)
	s.router.Use(s.rateLimitMiddleware)

	// Record HTTP request metrics (AI.md PART 20)
	s.router.Use(s.metricsMiddleware)

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

	// Public routes
	s.router.Get("/", s.handleHome)
	s.router.Get("/healthz", s.handleHealthz)

	// Prometheus metrics (internal only — firewall externally, AI.md PART 20)
	s.router.Get("/metrics", s.handleMetrics)

	// Special files (PWA, robots, security)
	s.router.Get("/robots.txt", s.handleRobotsTxt)
	s.router.Get("/security.txt", s.handleSecurityTxt)
	s.router.Get("/.well-known/security.txt", s.handleSecurityTxt)
	s.router.Get("/manifest.json", s.handleManifest)
	s.router.Get("/sw.js", s.handleServiceWorker)

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

	// OpenAPI/Swagger (root level)
	s.router.Get("/openapi", s.handleSwaggerUI)
	s.router.Get("/openapi.json", s.handleOpenAPIJSON)

	// GraphQL (root level)
	s.router.Get("/graphql", s.handleGraphiQLPage)
	s.router.Post("/graphql", s.handleGraphQL)
	s.router.Get("/graphiql", s.handleGraphiQLPage)

	// Static files
	s.router.Get("/static/*", s.handleStatic)
	s.router.Get("/favicon.ico", s.handleFavicon)

	// API v1 routes
	s.router.Route("/api/v1", func(r chi.Router) {
		// API info
		r.Get("/", s.handleAPIInfo)
		r.Get("/healthz", s.handleHealthz)
		r.Get("/healthz.txt", s.handleHealthzText)

		// Templates
		r.Get("/template/{name}", s.handleAPITemplate)
		r.Get("/template/{name}.txt", s.handleAPITemplateText)
		r.Get("/template/{name}.json", s.handleAPITemplateJSON)
		r.Get("/list", s.handleAPIList)
		r.Get("/list.txt", s.handleAPIListText)
		r.Get("/search", s.handleAPISearch)
		r.Get("/search.txt", s.handleAPISearchText)
		r.Get("/combine", s.handleAPICombine)
		r.Get("/combine.txt", s.handleAPICombineText)
		r.Get("/categories", s.handleAPICategories)
		r.Get("/categories.txt", s.handleAPICategoriesText)
		r.Get("/category/{name}", s.handleAPICategoryTemplates)
		r.Get("/category/{name}.txt", s.handleAPICategoryTemplatesText)
		r.Get("/stats", s.handleAPIStats)
		r.Get("/stats.txt", s.handleAPIStatsText)

		// Export
		r.Get("/templates.json", s.handleAPITemplatesJSON)
		r.Get("/templates.tar.gz", s.handleAPITemplatesTarGz)

		// Documentation
		r.Get("/docs", s.handleSwaggerUI)
		r.Get("/openapi.json", s.handleOpenAPIJSON)

		// GraphQL
		r.Post("/graphql", s.handleGraphQL)
		r.Get("/schema.graphql", s.handleGraphQLSchema)

		// CLI scripts
		r.Get("/cli/sh", s.handleCLIScriptSh)
		r.Get("/cli/ps", s.handleCLIScriptPs)
		r.Get("/cli/completion/bash", s.handleCLICompletionBash)
		r.Get("/cli/completion/zsh", s.handleCLICompletionZsh)
		r.Get("/cli/completion/fish", s.handleCLICompletionFish)
	})

	// gitignore.io route/API compatibility layer (unversioned, mounted
	// alongside /api/v1 — see IDEA.md "External API Compatibility")
	s.router.Get("/api/list", s.handleCompatList)
	s.router.Get("/api/{list}", s.handleCompatTemplates)

	// Debug routes (dev mode only)
	if s.config.DevMode {
		s.router.Get("/debug/routes", s.handleDebugRoutes)
		s.router.Get("/debug/config", s.handleDebugConfig)
		s.router.Get("/debug/templates", s.handleDebugTemplates)
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
			Enabled:   s.config.Cfg.Server.SSL.LetsEncrypt.Enabled,
			Email:     s.config.Cfg.Server.SSL.LetsEncrypt.Email,
			Challenge: s.config.Cfg.Server.SSL.LetsEncrypt.Challenge,
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

// detectServerURL determines the server URL from request headers
func (s *Server) detectServerURL(r *http.Request) string {
	// Check for reverse proxy headers
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		return fmt.Sprintf("%s://%s", proto, host)
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
