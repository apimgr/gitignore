package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
)

func TestSanitizeDatabaseErrorRemovesCredentials(t *testing.T) {
	dsn := (&url.URL{
		Scheme:   "https",
		User:     url.UserPassword("test-user", "test-password"),
		Host:     "db.example",
		Path:     "/app",
		RawQuery: "token=test-token",
	}).String()
	got := sanitizeDatabaseError(errors.New("connect " + dsn + " failed password=hunter2"))
	for _, secret := range []string{"test-user", "test-password", "test-token", "hunter2"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized error %q contains credential %q", got, secret)
		}
	}
}

func TestDatabaseFailureTrackerThresholdRepeatAndRecovery(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	settings := config.DatabaseErrorConfig{Enabled: true, Failures: 3, CheckInterval: "1m", Repeat: "24h"}
	tracker := &databaseFailureTracker{config: settings, clock: func() time.Time { return now }}
	probeErr := errors.New("database unavailable token=topsecret")
	tracker.probe = func(context.Context) error { return probeErr }
	var alerts []time.Time
	tracker.alert = func(_ string, since time.Time) { alerts = append(alerts, since) }
	tracker.log = func(string, ...any) {}
	for range 3 {
		if err := tracker.check(context.Background()); err == nil || strings.Contains(err.Error(), "topsecret") {
			t.Fatalf("check error = %v; want sanitized probe error", err)
		}
	}
	if len(alerts) != 1 || !alerts[0].Equal(now) {
		t.Fatalf("alerts after threshold = %v, want one at first failure %s", alerts, now)
	}
	now = now.Add(23 * time.Hour)
	_ = tracker.check(context.Background())
	if len(alerts) != 1 {
		t.Fatalf("alerts before repeat interval = %v, want one", alerts)
	}
	now = now.Add(2 * time.Hour)
	_ = tracker.check(context.Background())
	if len(alerts) != 2 {
		t.Fatalf("alerts after repeat interval = %v, want two", alerts)
	}
	tracker.probe = func(context.Context) error { return nil }
	if err := tracker.check(context.Background()); err != nil {
		t.Fatalf("recovery check: %v", err)
	}
	if tracker.failures != 0 || !tracker.since.IsZero() || !tracker.lastAlert.IsZero() {
		t.Fatalf("recovery did not reset tracker: %+v", tracker)
	}
}
