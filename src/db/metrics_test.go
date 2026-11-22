package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestQueryLabels(t *testing.T) {
	tests := []struct {
		query     string
		operation string
		table     string
	}{
		{"SELECT id FROM templates WHERE name = ?", "select", "templates"},
		{"INSERT INTO `api_tokens` (kind) VALUES (?)", "insert", "api_tokens"},
		{"UPDATE owners SET last_seen_at = ? WHERE id = ?", "update", "owners"},
		{"DELETE FROM security_reports WHERE tracking_id = ?", "delete", "security_reports"},
		{"WITH selected AS (SELECT id FROM owners) SELECT id FROM selected", "select", "owners"},
		{"PRAGMA journal_mode=WAL", "select", "unknown"},
	}
	for _, test := range tests {
		t.Run(test.operation+"/"+test.table, func(t *testing.T) {
			operation, table := queryLabels(test.query)
			if operation != test.operation || table != test.table {
				t.Fatalf("queryLabels(%q) = (%q, %q), want (%q, %q)", test.query, operation, table, test.operation, test.table)
			}
		})
	}
}

func TestMetricsDriverObservesQueries(t *testing.T) {
	type event struct {
		operation string
		table     string
		duration  time.Duration
		err       error
	}
	events := make(chan event, 2)
	SetQueryObserver(func(operation, table string, duration time.Duration, err error) {
		events <- event{operation: operation, table: table, duration: duration, err: err}
	})
	t.Cleanup(func() { SetQueryObserver(nil) })

	conn, err := sql.Open(metricsDB, ":memory:")
	if err != nil {
		t.Fatalf("open metrics driver: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(context.Background(), "CREATE TABLE metric_probe (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), "INSERT INTO metric_probe (id) VALUES (?)", 1); err != nil {
		t.Fatalf("insert probe row: %v", err)
	}
	select {
	case got := <-events:
		if got.operation != "select" || got.table != "unknown" {
			t.Fatalf("DDL event = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no metrics event for DDL")
	}
	select {
	case got := <-events:
		if got.operation != "insert" || got.table != "metric_probe" || got.err != nil || got.duration < 0 {
			t.Fatalf("insert event = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no metrics event for INSERT")
	}
}

func TestQueryErrorType(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{errors.New("UNIQUE constraint failed"), "duplicate"},
		{errors.New("FOREIGN KEY constraint failed"), "constraint"},
		{errors.New("database connection closed"), "connection"},
		{errors.New("unexpected failure"), "other"},
	}
	for _, test := range tests {
		if got := QueryErrorType(test.err); got != test.want {
			t.Errorf("QueryErrorType(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
