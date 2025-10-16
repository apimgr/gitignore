package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apimgr/gitignore/src/database"
	"github.com/apimgr/gitignore/src/paths"
	"github.com/apimgr/gitignore/src/templates"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Config holds server configuration
type Config struct {
	Address     string
	Port        int
	DevMode     bool
	Database    *database.Database
	Templates   *templates.Manager
	Paths       *paths.PathManager
	Version     string
	Commit      string
	BuildDate   string
}

// Server represents the HTTP server
type Server struct {
	config *Config
	router *chi.Mux
	server *http.Server
}

// New creates a new server instance
func New(config *Config) *Server {
	s := &Server{
		config: config,
		router: chi.NewRouter(),
	}

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

	// Timeout
	s.router.Use(middleware.Timeout(30 * time.Second))

	// Compression
	s.router.Use(middleware.Compress(5))

	// CORS
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders: []string{"Link"},
		MaxAge:         300,
	})
	s.router.Use(corsHandler.Handler)

	// Reverse proxy header detection
	s.router.Use(s.reverseProxyMiddleware)
}

// setupRoutes configures all routes
func (s *Server) setupRoutes() {
	// Public routes
	s.router.Get("/", s.handleHome)
	s.router.Get("/healthz", s.handleHealthz)

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

	// GraphiQL
	s.router.Get("/graphiql", s.handleGraphiQLPage)

	// Static files
	s.router.Get("/static/*", s.handleStatic)
	s.router.Get("/favicon.ico", s.handleFavicon)
	s.router.Get("/robots.txt", s.handleRobotsTxt)

	// API v1 routes
	s.router.Route("/api/v1", func(r chi.Router) {
		// API info
		r.Get("/", s.handleAPIInfo)
		r.Get("/healthz", s.handleHealthz)
		r.Get("/healthz.txt", s.handleHealthzText)

		// Templates
		r.Get("/template/{name}", s.handleAPITemplate)
		r.Get("/template/{name}.json", s.handleAPITemplateJSON)
		r.Get("/list", s.handleAPIList)
		r.Get("/search", s.handleAPISearch)
		r.Get("/combine", s.handleAPICombine)
		r.Get("/categories", s.handleAPICategories)
		r.Get("/category/{name}", s.handleAPICategoryTemplates)
		r.Get("/stats", s.handleAPIStats)

		// Export
		r.Get("/templates.json", s.handleAPITemplatesJSON)
		r.Get("/templates.tar.gz", s.handleAPITemplatesTarGz)

		// Documentation
		r.Get("/docs", s.handleSwaggerUI)
		r.Get("/openapi.json", s.handleOpenAPIJSON)
		r.Get("/openapi.yaml", s.handleOpenAPIYAML)

		// GraphQL
		r.Post("/graphql", s.handleGraphQL)
		r.Get("/schema.graphql", s.handleGraphQLSchema)

		// CLI scripts
		r.Get("/cli/sh", s.handleCLIScriptSh)
		r.Get("/cli/ps", s.handleCLIScriptPs)
		r.Get("/cli/completion/bash", s.handleCLICompletionBash)
		r.Get("/cli/completion/zsh", s.handleCLICompletionZsh)
		r.Get("/cli/completion/fish", s.handleCLICompletionFish)

		// Admin routes (protected)
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.adminAuthMiddleware)

			r.Get("/", s.handleAdminAPIInfo)
			r.Get("/healthz", s.handleAdminHealthz)
			r.Get("/settings", s.handleAdminGetSettings)
			r.Put("/settings", s.handleAdminUpdateSettings)
			r.Get("/database", s.handleAdminDatabaseStatus)
			r.Post("/database/test", s.handleAdminDatabaseTest)
			r.Get("/logs", s.handleAdminListLogs)
			r.Get("/logs/{type}", s.handleAdminGetLog)
			r.Get("/backup", s.handleAdminListBackups)
			r.Post("/backup", s.handleAdminCreateBackup)
			r.Delete("/backup/{id}", s.handleAdminDeleteBackup)
		})
	})

	// Admin web routes (protected with Basic Auth)
	s.router.Route("/admin", func(r chi.Router) {
		r.Use(s.basicAuthMiddleware)

		r.Get("/", s.handleAdminDashboard)
		r.Get("/settings", s.handleAdminSettingsPage)
		r.Post("/settings", s.handleAdminUpdateSettingsForm)
		r.Get("/database", s.handleAdminDatabasePage)
		r.Post("/database/test", s.handleAdminDatabaseTestForm)
		r.Get("/logs", s.handleAdminLogsPage)
		r.Get("/logs/{type}", s.handleAdminLogView)
		r.Get("/backup", s.handleAdminBackupPage)
		r.Post("/backup/create", s.handleAdminCreateBackupForm)
		r.Post("/backup/restore", s.handleAdminRestoreBackupForm)
		r.Get("/healthz", s.handleAdminHealthPage)
	})

	// Debug routes (dev mode only)
	if s.config.DevMode {
		s.router.Get("/debug/routes", s.handleDebugRoutes)
		s.router.Get("/debug/config", s.handleDebugConfig)
		s.router.Get("/debug/db", s.handleDebugDB)
		s.router.Get("/debug/templates", s.handleDebugTemplates)
		s.router.Post("/debug/reset", s.handleDebugReset)
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
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
	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	log.Println("Server stopped")
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
