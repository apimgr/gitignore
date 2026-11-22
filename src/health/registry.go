// Package health provides an ordered registry of cached dataset and dependency checks.
package health

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

var validKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Result is the public-safe cached state of a registered dataset check.
type Result struct {
	Key      string
	State    string
	Critical bool
}

type check struct {
	key      string
	critical bool
	probe    func() string
}

// Registry stores check callbacks in registration order and their last cached values.
type Registry struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	checks    []check
	byKey     map[string]int
	values    []string
	refreshed time.Time
}

// NewRegistry constructs an empty registry.
func NewRegistry() *Registry {
	return &Registry{byKey: make(map[string]int)}
}

// Register adds a stable public-safe check exactly once.
func (r *Registry) Register(key string, critical bool, probe func() string) error {
	if !validKey.MatchString(key) || probe == nil {
		return errors.New("health: invalid check registration")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byKey[key]; exists {
		return errors.New("health: duplicate check key")
	}
	r.byKey[key] = len(r.checks)
	r.checks = append(r.checks, check{key: key, critical: critical, probe: probe})
	r.values = append(r.values, "error")
	return nil
}

// Refresh updates cached values no more than once per minute.
func (r *Registry) Refresh() {
	r.refresh(time.Now().UTC(), false)
}

// RefreshNow refreshes the cache immediately after a data-set update or reload.
func (r *Registry) RefreshNow() {
	r.refresh(time.Now().UTC(), true)
}

func (r *Registry) refresh(now time.Time, force bool) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.mu.RLock()
	if !force && !r.refreshed.IsZero() && now.Sub(r.refreshed) < time.Minute {
		r.mu.RUnlock()
		return
	}
	checks := append([]check(nil), r.checks...)
	r.mu.RUnlock()

	values := make([]string, len(checks))
	for i, item := range checks {
		values[i] = safeState(item.probe)
	}
	r.mu.Lock()
	r.values = values
	r.refreshed = now
	r.mu.Unlock()
}

func safeState(probe func() string) (state string) {
	state = "error"
	defer func() {
		if recover() != nil {
			state = "error"
		}
	}()
	state = strings.TrimSpace(probe())
	switch state {
	case "ok", "degraded", "error":
		return state
	default:
		return "error"
	}
}

// Snapshot returns cached results in their stable registration order.
func (r *Registry) Snapshot() []Result {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]Result, len(r.checks))
	for i, item := range r.checks {
		state := "error"
		if i < len(r.values) {
			state = r.values[i]
		}
		results[i] = Result{Key: item.key, State: state, Critical: item.critical}
	}
	return results
}
