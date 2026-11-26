package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
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

func TestMapServerIdentifiers(t *testing.T) {
	query := "CREATE INDEX IF NOT EXISTS idx_audit_category ON audit_log(category); INSERT INTO owners(id) VALUES ('owners'); -- scheduler_tasks\nSELECT id FROM \"scheduler_tasks\""
	want := "CREATE INDEX IF NOT EXISTS srv_idx_audit_category ON srv_audit_log(category); INSERT INTO srv_owners(id) VALUES ('owners'); -- scheduler_tasks\nSELECT id FROM \"srv_scheduler_tasks\""
	if got := mapServerIdentifiers(query, true); got != want {
		t.Fatalf("libSQL identifier mapping = %q, want %q", got, want)
	}
	if got := mapServerIdentifiers(query, false); got != query {
		t.Fatalf("SQLite identifier mapping changed query: %q", got)
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

func TestMetricsDriverRefreshesCacheAfterTransactionCommit(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	connection, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(context.Background(), "CREATE TABLE metric_probe (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	var wrapped *instrumentedConn
	if err := connection.Raw(func(raw any) error {
		conn, ok := raw.(driver.Conn)
		if !ok {
			t.Fatalf("raw connection has type %T, want driver.Conn", raw)
		}
		wrapped = &instrumentedConn{Conn: conn, prefix: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	refreshes := 0
	wrapped.refresh = func(context.Context) { refreshes++ }

	ctx := context.Background()
	rolledBack, err := wrapped.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.ExecContext(ctx, "INSERT INTO metric_probe (id) VALUES (?)", []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}); err != nil {
		t.Fatal(err)
	}
	if refreshes != 0 {
		t.Fatalf("cache refreshed before rollback: %d refreshes", refreshes)
	}
	if err := rolledBack.Rollback(); err != nil {
		t.Fatal(err)
	}
	if refreshes != 0 {
		t.Fatalf("cache refreshed after rollback: %d refreshes", refreshes)
	}

	committed, err := wrapped.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	statement, err := wrapped.Prepare("INSERT INTO metric_probe (id) VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = statement.Close() }()
	prepared := statement.(*instrumentedStmt)
	if _, err := prepared.ExecContext(ctx, []driver.NamedValue{{Ordinal: 1, Value: int64(2)}}); err != nil {
		t.Fatal(err)
	}
	if refreshes != 0 {
		t.Fatalf("cache refreshed before commit: %d refreshes", refreshes)
	}
	if err := committed.Commit(); err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 {
		t.Fatalf("cache refreshes after commit = %d, want 1", refreshes)
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
