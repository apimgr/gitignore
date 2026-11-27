package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

var databaseCredentialPattern = regexp.MustCompile(`(?i)(password|passwd|token|secret|credential|authorization|auth|api[_-]?key|access[_-]?token)=([^&\s]+)`)
var databaseURLCredentialPattern = regexp.MustCompile(`(?i)(https?://)[^/@\s:]+:[^/@\s]+@`)

func sanitizeDatabaseError(err error) string {
	if err == nil {
		return "database probe failed"
	}
	message := databaseCredentialPattern.ReplaceAllString(err.Error(), "$1=[REDACTED]")
	message = databaseURLCredentialPattern.ReplaceAllString(message, "${1}[REDACTED]@")
	if parsed, parseErr := url.Parse(message); parseErr == nil && parsed.User != nil {
		parsed.User = url.UserPassword("[REDACTED]", "[REDACTED]")
		message = parsed.String()
	}
	return strings.TrimSpace(message)
}

type databaseFailureTracker struct {
	mu        sync.Mutex
	failures  int
	since     time.Time
	lastAlert time.Time
	config    config.DatabaseErrorConfig
	clock     func() time.Time
	probe     func(context.Context) error
	alert     func(string, time.Time)
	log       func(string, ...any)
}

func newDatabaseCheckHandler(settings config.DatabaseErrorConfig) func(context.Context) error {
	tracker := &databaseFailureTracker{
		config: settings,
		clock:  time.Now,
		probe:  db.Probe,
		alert:  emitDatabaseError,
		log:    log.Printf,
	}
	return tracker.check
}

func (t *databaseFailureTracker) check(ctx context.Context) error {
	err := t.probe(ctx)
	now := t.clock().UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil {
		if t.failures > 0 {
			t.log("INFO Database connection recovered after %d failed probes", t.failures)
		}
		t.failures = 0
		t.since = time.Time{}
		t.lastAlert = time.Time{}
		return nil
	}
	if t.failures == 0 {
		t.since = now
	}
	t.failures++
	clean := sanitizeDatabaseError(err)
	threshold := t.config.Failures
	if threshold < 1 {
		threshold = 3
	}
	if t.config.Enabled && t.failures >= threshold {
		repeat, parseErr := time.ParseDuration(t.config.Repeat)
		if parseErr != nil || repeat <= 0 {
			repeat = 24 * time.Hour
		}
		if t.lastAlert.IsZero() || now.Sub(t.lastAlert) >= repeat {
			t.log("WARN Database connection unavailable: failures=%d since=%s error=%s", t.failures, t.since.Format(time.RFC3339), clean)
			t.alert(clean, t.since)
			t.lastAlert = now
		}
	}
	return fmt.Errorf("database connection probe failed: %s", clean)
}
