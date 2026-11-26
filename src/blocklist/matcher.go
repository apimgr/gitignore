package blocklist

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const freshnessWindow = 72 * time.Hour

type SourceStatus struct {
	ID          string
	Kind        string
	State       string
	Entries     int
	LastUpdated time.Time
}

type loadedSource struct {
	kind    string
	entries map[string]struct{}
	updated time.Time
}

// Matcher provides a lock-safe, on-disk-backed view of configured blocklists.
type Matcher struct {
	dir     string
	sources []Source
	mu      sync.RWMutex
	loaded  map[string]loadedSource
	last    time.Time
}

// NewMatcher configures the built-in FireHOL source plus every enabled extra.
func NewMatcher(securityDir string, extras []Source) *Matcher {
	sources := []Source{{ID: FireHOLID, Type: "ip"}}
	for _, source := range extras {
		if source.Enabled != nil && !*source.Enabled {
			continue
		}
		sources = append(sources, source)
	}
	return &Matcher{dir: filepath.Join(securityDir, "blocklists"), sources: sources, loaded: map[string]loadedSource{}}
}

// Reload loads every configured source and preserves the last good in-memory
// source when a file is unavailable or invalid.
func (m *Matcher) Reload(now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	updated := make(map[string]loadedSource, len(m.sources))
	var failures []error
	for _, source := range m.sources {
		if err := validateSource(source); source.ID != FireHOLID && err != nil {
			failures = append(failures, err)
			continue
		}
		path := filepath.Join(m.dir, source.ID+".txt")
		data, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", source.ID, err))
			continue
		}
		entries, _, err := parseEntries(data, source.Type, nil)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", source.ID, err))
			continue
		}
		stamp, err := os.ReadFile(filepath.Join(m.dir, source.ID+".last_updated"))
		if err != nil {
			failures = append(failures, fmt.Errorf("%s update timestamp: %w", source.ID, err))
			continue
		}
		lastUpdated, err := time.Parse(time.RFC3339, strings.TrimSpace(string(stamp)))
		if err != nil {
			failures = append(failures, fmt.Errorf("%s has invalid update timestamp", source.ID))
			continue
		}
		entrySet := make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			entrySet[entry] = struct{}{}
		}
		updated[source.ID] = loadedSource{kind: source.Type, entries: entrySet, updated: lastUpdated.UTC()}
	}
	m.mu.Lock()
	for id, source := range updated {
		m.loaded[id] = source
	}
	m.last = now
	m.mu.Unlock()
	return errors.Join(failures...)
}

// ensureReload refreshes the snapshot at most once per minute.
func (m *Matcher) ensureReload(now time.Time) {
	m.mu.RLock()
	last := m.last
	m.mu.RUnlock()
	if !last.IsZero() && now.Sub(last) < time.Minute {
		return
	}
	_ = m.Reload(now)
}

// MatchIP reports the first active source that blocks the address.
func (m *Matcher) MatchIP(value string, now time.Time) (string, bool) {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	m.ensureReload(now)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, source := range m.sources {
		loaded, ok := m.loaded[source.ID]
		if !ok || loaded.kind != "ip" || now.Sub(loaded.updated) > freshnessWindow {
			continue
		}
		for entry := range loaded.entries {
			prefix, valid := parsePrefix(entry)
			if valid && prefix.Contains(address.Unmap()) {
				return source.ID, true
			}
		}
	}
	return "", false
}

// MatchDomain reports the first active source that blocks a normalized host.
func (m *Matcher) MatchDomain(value string, now time.Time) (string, bool) {
	host := HostFromURL(value)
	if host == "" {
		host = strings.TrimSpace(value)
	}
	normalized, valid := normalizeDomainEntry(strings.TrimSuffix(strings.ToLower(host), "."))
	if !valid || strings.HasPrefix(normalized, "*.") {
		return "", false
	}
	m.ensureReload(now)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, source := range m.sources {
		loaded, ok := m.loaded[source.ID]
		if !ok || loaded.kind != "domain" || now.Sub(loaded.updated) > freshnessWindow {
			continue
		}
		for entry := range loaded.entries {
			if DomainMatches(entry, normalized) {
				return source.ID, true
			}
		}
	}
	return "", false
}

// Status reports whether each source has ever loaded and whether its data is
// fresh within the fixed 72-hour window.
func (m *Matcher) Status(now time.Time) []SourceStatus {
	m.ensureReload(now)
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]SourceStatus, 0, len(m.sources))
	for _, source := range m.sources {
		status := SourceStatus{ID: source.ID, Kind: source.Type, State: "error"}
		if loaded, ok := m.loaded[source.ID]; ok {
			status.Entries = len(loaded.entries)
			status.LastUpdated = loaded.updated
			status.State = "ok"
			if now.Sub(loaded.updated) > freshnessWindow {
				status.State = "degraded"
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}
