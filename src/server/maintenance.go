package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
)

// MaintenanceState carries the current recoverable critical-error state.
type MaintenanceState struct {
	mu       sync.RWMutex
	snapshot MaintenanceSnapshot
	observer func(string, MaintenanceSnapshot)
}

// MaintenanceSnapshot is a credential-free snapshot safe for status output.
type MaintenanceSnapshot struct {
	Active      bool
	Reason      string
	Message     string
	Since       time.Time
	Attempts    int
	LastAttempt *time.Time
	NextAttempt *time.Time
	Exhausted   bool
}

// Enter switches the service into maintenance mode, preserving its original
// start time when the same incident is updated.
func (m *MaintenanceState) Enter(reason, message string, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	entered := !m.snapshot.Active
	if !m.snapshot.Active {
		m.snapshot = MaintenanceSnapshot{Since: now.UTC()}
	}
	m.snapshot.Active = true
	m.snapshot.Reason = reason
	m.snapshot.Message = message
	m.snapshot.Exhausted = false
	snapshot, observer := m.snapshot, m.observer
	m.mu.Unlock()
	if entered && observer != nil {
		observer("server.maintenance_entered", snapshot)
	}
}

// RecordAttempt updates retry metadata while leaving maintenance active.
func (m *MaintenanceState) RecordAttempt(attempt int, last, next *time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.Attempts = attempt
	m.snapshot.LastAttempt = cloneTime(last)
	m.snapshot.NextAttempt = cloneTime(next)
}

// Exhaust marks retries as stopped while keeping writes unavailable.
func (m *MaintenanceState) Exhaust(attempts int, last *time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	newlyExhausted := !m.snapshot.Exhausted
	m.snapshot.Active = true
	m.snapshot.Attempts = attempts
	m.snapshot.LastAttempt = cloneTime(last)
	m.snapshot.NextAttempt = nil
	m.snapshot.Exhausted = true
	snapshot, observer := m.snapshot, m.observer
	m.mu.Unlock()
	if newlyExhausted && observer != nil {
		observer("server.self_healing_exhausted", snapshot)
	}
}

// Recover clears maintenance state after health and read/write checks pass.
func (m *MaintenanceState) Recover() MaintenanceSnapshot {
	if m == nil {
		return MaintenanceSnapshot{}
	}
	m.mu.Lock()
	previous := m.snapshot
	m.snapshot = MaintenanceSnapshot{}
	observer := m.observer
	m.mu.Unlock()
	if previous.Active && observer != nil {
		observer("server.maintenance_recovered", previous)
	}
	return previous
}

// SetObserver installs a callback for maintenance state transitions.
func (m *MaintenanceState) SetObserver(observer func(string, MaintenanceSnapshot)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.observer = observer
	m.mu.Unlock()
}

// Snapshot returns a consistent copy of current maintenance metadata.
func (m *MaintenanceState) Snapshot() MaintenanceSnapshot {
	if m == nil {
		return MaintenanceSnapshot{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	copy := m.snapshot
	copy.LastAttempt = cloneTime(copy.LastAttempt)
	copy.NextAttempt = cloneTime(copy.NextAttempt)
	return copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

// maintenanceMiddleware rejects state-changing requests while preserving
// read-only GraphQL and MCP POST operations.
func (s *Server) maintenanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config == nil || s.config.MaintenanceState == nil || !s.config.MaintenanceState.Snapshot().Active || !maintenanceWriteRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		snapshot := s.config.MaintenanceState.Snapshot()
		retrySeconds := 30
		if cfg := s.currentConfig(); cfg != nil && cfg.Server.Maintenance.SelfHealing.RetryInterval > 0 {
			retrySeconds = int(cfg.Server.Maintenance.SelfHealing.RetryInterval)
		}
		w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
		w.Header().Set("X-Maintenance-Mode", "true")
		w.Header().Set("X-Maintenance-Reason", snapshot.Reason)
		details := map[string]interface{}{"reason": snapshot.Reason, "self_healing": !snapshot.Exhausted}
		setCacheHeaders(w, "error")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = encodeJSON(w, APIResponse{OK: false, Error: "MAINTENANCE", Message: i18n.T(r, "errors.maintenance_mode"), Details: details})
	})
}

func maintenanceWriteRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if strings.HasSuffix(r.URL.Path, "/graphql") || strings.HasSuffix(r.URL.Path, "/mcp") {
		return false
	}
	return r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
}

// maintenanceHealth translates the internal snapshot into the stable health
// response contract without exposing filesystem paths or raw errors.
func maintenanceHealth(snapshot MaintenanceSnapshot) *MaintenanceHealth {
	if !snapshot.Active {
		return nil
	}
	health := &MaintenanceHealth{
		Reason:  snapshot.Reason,
		Message: snapshot.Message,
		Since:   snapshot.Since,
		SelfHealing: SelfHealingHealth{
			Enabled:   !snapshot.Exhausted,
			Exhausted: snapshot.Exhausted,
			Attempts:  snapshot.Attempts,
			Last:      cloneTime(snapshot.LastAttempt),
			Next:      cloneTime(snapshot.NextAttempt),
		},
	}
	return health
}

func (s *Server) handleMaintenanceStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()
	raw, ok := parseBearerHeader(r.Header.Get("Authorization"))
	if !ok || cfg == nil || cfg.Server.Token == "" {
		sendAPIResponseErrorLocalized(w, r, "UNAUTHORIZED")
		return
	}
	presented := sha256.Sum256([]byte(raw))
	configured := sha256.Sum256([]byte(cfg.Server.Token))
	if subtle.ConstantTimeCompare(presented[:], configured[:]) != 1 {
		sendAPIResponseErrorLocalized(w, r, "UNAUTHORIZED")
		return
	}
	snapshot := MaintenanceSnapshot{}
	if s.config != nil && s.config.MaintenanceState != nil {
		snapshot = s.config.MaintenanceState.Snapshot()
	}
	setCacheHeaders(w, "error")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	status := http.StatusOK
	if snapshot.Active {
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	_ = encodeJSON(w, map[string]any{
		"active":       snapshot.Active,
		"reason":       snapshot.Reason,
		"message":      snapshot.Message,
		"since":        snapshot.Since,
		"self_healing": maintenanceHealth(snapshot),
		"guidance":     "Check database connectivity and permissions for configured runtime directories. Fix the underlying issue; automatic recovery will resume when checks pass.",
	})
}
