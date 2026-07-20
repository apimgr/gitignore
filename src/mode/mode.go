// Package mode implements application mode detection and configuration.
// It provides Production and Development modes with different settings for
// logging, debugging, error handling, caching, and profiling.
package mode

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/apimgr/gitignore/src/config"
)

const (
	appName = "gitignore"
)

// Mode represents the application execution mode
type Mode string

const (
	// Production mode: optimized for production use with security and performance
	Production Mode = "production"

	// Development mode: optimized for development with debugging and verbose output
	Development Mode = "development"
)

var (
	// current holds the current application mode
	current Mode = Production

	// debug holds the independent debug-flag state (--debug / DEBUG=true).
	// Debug is tracked independently of mode: mode and debug are two
	// independent axes producing four operational states (see AI.md PART 6).
	debug bool

	// mu protects concurrent access to current mode and debug
	mu sync.RWMutex
)

// Get returns the current application mode
func Get() Mode {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Set sets the application mode.
// "debug" is an alias for development mode + debug on (an explicit
// SetDebug/--debug flag or DEBUG env var applied afterward still wins).
func Set(mode string) error {
	normalized := strings.ToLower(strings.TrimSpace(mode))

	parsed, err := ParseMode(normalized)
	if err != nil {
		return err
	}

	mu.Lock()
	current = parsed
	mu.Unlock()

	if normalized == "debug" {
		SetDebug(true)
	}

	return nil
}

// ParseMode parses a mode string and returns the corresponding Mode constant.
// Accepts: "dev", "development", "prod", "production" (case-insensitive).
// "debug" is also accepted as an alias for Development (see Set).
func ParseMode(s string) (Mode, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	switch s {
	case "dev", "development":
		return Development, nil
	case "debug":
		return Development, nil
	case "prod", "production":
		return Production, nil
	default:
		return "", fmt.Errorf("invalid mode: %q (must be one of: dev, development, prod, production, debug)", s)
	}
}

// IsDevelopment returns true if the current mode is Development
func IsDevelopment() bool {
	return Get() == Development
}

// IsProduction returns true if the current mode is Production
func IsProduction() bool {
	return Get() == Production
}

// IsDebug returns true if the independent debug flag is enabled
// (--debug CLI flag, DEBUG=true env var, or the "debug" mode alias).
// Debug is tracked independently of mode: mode and debug are two
// independent axes producing four operational states (see AI.md PART 6).
func IsDebug() bool {
	mu.RLock()
	defer mu.RUnlock()
	return debug
}

// SetDebug sets the independent debug flag. It does not change the
// application mode. Debug affects verbosity and diagnostics ONLY — it
// never disables authentication or security checks, in any mode.
func SetDebug(enabled bool) {
	mu.Lock()
	debug = enabled
	mu.Unlock()
}

// Init initializes the mode based on environment variables.
// This should be called before CLI flag parsing so flags can override it.
func Init() {
	if modeEnv := os.Getenv("MODE"); modeEnv != "" {
		if err := Set(modeEnv); err != nil {
			// If invalid mode in env var, log warning but keep default
			fmt.Fprintf(os.Stderr, "Warning: %v, using default: %s\n", err, Production)
		}
	}
}

// InitDebug initializes the independent debug flag from the DEBUG
// environment variable. This should be called after Init (so the
// "debug" mode alias is applied first) and before CLI flag parsing so
// --debug can still override it. An explicitly set DEBUG env var
// (truthy OR falsy) always wins over the "debug" mode alias —
// MODE=debug DEBUG=false runs development mode with debug off.
func InitDebug() {
	v, set := os.LookupEnv("DEBUG")
	if !set {
		return
	}

	parsed, err := config.ParseBool(v, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: invalid DEBUG value: %q, ignoring\n", v)
		return
	}

	SetDebug(parsed)
}

// GetErrorDetail returns error details based on the current mode.
// In development mode, returns full error details with stack traces.
// In production mode, returns generic error message without internal details.
func GetErrorDetail(err error) string {
	if err == nil {
		return ""
	}

	if IsDevelopment() {
		// In development, return full error details
		return fmt.Sprintf("%+v", err)
	}

	// In production, return generic error message
	return "An internal error occurred. Please try again later."
}

// ShouldShowDebugEndpoints returns true if debug endpoints should be enabled.
// Debug endpoints include /debug/pprof/* and /debug/vars.
// These are gated by the independent debug flag (--debug/DEBUG=true) in
// BOTH production and development mode — development mode alone does
// NOT enable them (see AI.md PART 6).
func ShouldShowDebugEndpoints() bool {
	return IsDebug()
}

// GetCacheHeaders returns appropriate cache control headers for static files
// based on the current mode.
// Development: no-cache (always revalidate)
// Production: appropriate caching for performance
func GetCacheHeaders() map[string]string {
	if IsDevelopment() {
		// In development, disable caching to see changes immediately
		return map[string]string{
			"Cache-Control": "no-cache, no-store, must-revalidate",
			"Pragma":        "no-cache",
			"Expires":       "0",
		}
	}

	// In production, enable caching for performance
	// Cache for 1 hour with revalidation
	return map[string]string{
		"Cache-Control": "public, max-age=3600, must-revalidate",
	}
}

// GetLogLevel returns the recommended log level for the current mode.
// Development: "debug"
// Production: "info"
func GetLogLevel() string {
	if IsDevelopment() {
		return "debug"
	}
	return "info"
}

// ShouldCacheTemplates returns true if templates should be cached.
// Development: false (reload templates on each request)
// Production: true (cache compiled templates)
func ShouldCacheTemplates() bool {
	return IsProduction()
}

// ShouldCacheStaticFiles returns true if static files should be cached.
// Development: false (reload static files on each request)
// Production: true (cache static files in memory)
func ShouldCacheStaticFiles() bool {
	return IsProduction()
}

// ShouldEnableAutoReload returns true if auto-reload should be enabled.
// Development: true (watch for file changes and reload)
// Production: false (no auto-reload)
func ShouldEnableAutoReload() bool {
	return IsDevelopment()
}

// ShouldEnableProfiling returns true if profiling endpoints should be enabled.
// Gated by the independent debug flag (--debug/DEBUG=true), not by mode
// alone — pprof endpoints stay disabled in development mode until debug
// is explicitly enabled (see AI.md PART 6).
func ShouldEnableProfiling() bool {
	return IsDebug()
}

// GetPanicRecoveryMode returns the panic recovery behavior.
// Development: "verbose" (full stack trace in response)
// Production: "graceful" (log error, return 500, continue)
func GetPanicRecoveryMode() string {
	if IsDevelopment() {
		return "verbose"
	}
	return "graceful"
}

// String returns the string representation of the mode
func (m Mode) String() string {
	return string(m)
}
