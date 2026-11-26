package db

import (
	"testing"
	"time"
)

func setupSchedulerTestDB(t *testing.T) {
	t.Helper()
	dataDir := t.TempDir()
	if err := Init(dataDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
}

func TestUpsertAndGetSchedulerState(t *testing.T) {
	setupSchedulerTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	st := SchedulerState{
		TaskID:     "token_cleanup",
		TaskName:   "Token Cleanup",
		Schedule:   "@daily",
		LastRun:    now,
		LastStatus: "ok",
		NextRun:    now.Add(24 * time.Hour),
		RunCount:   1,
		FailCount:  0,
		Enabled:    true,
	}
	if err := UpsertSchedulerState(st); err != nil {
		t.Fatalf("UpsertSchedulerState: %v", err)
	}

	got, err := GetSchedulerState("token_cleanup")
	if err != nil {
		t.Fatalf("GetSchedulerState: %v", err)
	}
	if got == nil {
		t.Fatal("GetSchedulerState returned nil for existing task")
	}
	if got.TaskName != "Token Cleanup" || got.Schedule != "@daily" {
		t.Errorf("state mismatch: %+v", got)
	}
	if !got.LastRun.Equal(now) {
		t.Errorf("LastRun = %v, want %v", got.LastRun, now)
	}
	if got.RunCount != 1 || got.FailCount != 0 || !got.Enabled {
		t.Errorf("counters/enabled mismatch: %+v", got)
	}
}

func TestUpsertSchedulerStateUpdatesExisting(t *testing.T) {
	setupSchedulerTestDB(t)

	base := SchedulerState{TaskID: "t1", TaskName: "one", Schedule: "@hourly", RunCount: 1, Enabled: true}
	if err := UpsertSchedulerState(base); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	updated := base
	updated.RunCount = 5
	updated.LastStatus = "failed"
	updated.FailCount = 2
	if err := UpsertSchedulerState(updated); err != nil {
		t.Fatalf("update upsert: %v", err)
	}

	got, err := GetSchedulerState("t1")
	if err != nil {
		t.Fatalf("GetSchedulerState: %v", err)
	}
	if got.RunCount != 5 || got.FailCount != 2 || got.LastStatus != "failed" {
		t.Errorf("expected updated state, got %+v", got)
	}

	states, err := LoadSchedulerStates()
	if err != nil {
		t.Fatalf("LoadSchedulerStates: %v", err)
	}
	if len(states) != 1 {
		t.Errorf("expected exactly 1 state row after update-in-place, got %d", len(states))
	}
}

func TestGetSchedulerStateMissingReturnsNilNoError(t *testing.T) {
	setupSchedulerTestDB(t)

	got, err := GetSchedulerState("does-not-exist")
	if err != nil {
		t.Fatalf("GetSchedulerState: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing task, got %+v", got)
	}
}

func TestLoadSchedulerStatesEmpty(t *testing.T) {
	setupSchedulerTestDB(t)

	states, err := LoadSchedulerStates()
	if err != nil {
		t.Fatalf("LoadSchedulerStates: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected empty states, got %d", len(states))
	}
}

func TestSetSchedulerEnabled(t *testing.T) {
	setupSchedulerTestDB(t)

	if err := UpsertSchedulerState(SchedulerState{TaskID: "t1", Enabled: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	found, err := SetSchedulerEnabled("t1", false)
	if err != nil {
		t.Fatalf("SetSchedulerEnabled: %v", err)
	}
	if !found {
		t.Error("expected found=true for existing task")
	}

	got, err := GetSchedulerState("t1")
	if err != nil {
		t.Fatalf("GetSchedulerState: %v", err)
	}
	if got.Enabled {
		t.Error("expected task to be disabled after SetSchedulerEnabled(false)")
	}
}

func TestSetSchedulerEnabledMissingTaskReturnsFalse(t *testing.T) {
	setupSchedulerTestDB(t)

	found, err := SetSchedulerEnabled("nope", true)
	if err != nil {
		t.Fatalf("SetSchedulerEnabled: %v", err)
	}
	if found {
		t.Error("expected found=false for missing task")
	}
}

func TestCleanupExpiredTokens(t *testing.T) {
	setupSchedulerTestDB(t)
	past := time.Now().UTC().Unix() - 7200
	if _, err := conn.Exec(`INSERT INTO rate_limits(key, count, window_start, updated_at) VALUES ('stale', 1, ?, ?)`, past, past); err != nil {
		t.Fatalf("seed stale rate counter: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO rate_limit_events(key, occurred_at) VALUES ('stale', ?)`, past*1000); err != nil {
		t.Fatalf("seed stale rate event: %v", err)
	}

	n, err := CleanupExpiredTokens()
	if err != nil {
		t.Fatalf("CleanupExpiredTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("CleanupExpiredTokens() = %d, want 2 stale rate records", n)
	}
	var remaining int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM rate_limit_events`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining rate events: %v", err)
	}
	if remaining != 0 {
		t.Errorf("expected stale rate event removed, got %d remaining", remaining)
	}
}

func TestCleanupExpiredTokensNoRows(t *testing.T) {
	setupSchedulerTestDB(t)

	n, err := CleanupExpiredTokens()
	if err != nil {
		t.Fatalf("CleanupExpiredTokens: %v", err)
	}
	if n != 0 {
		t.Errorf("CleanupExpiredTokens() = %d, want 0 on empty db", n)
	}
}

func TestPing(t *testing.T) {
	setupSchedulerTestDB(t)
	if err := Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
