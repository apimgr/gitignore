package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
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

func TestDebugCacheObserverReceivesBoundedCacheLabel(t *testing.T) {
	type observation struct {
		operation, key string
		hit            bool
		duration       time.Duration
	}
	observed := make(chan observation, 1)
	SetDebugCacheObserver(func(operation, key string, hit bool, duration time.Duration) {
		observed <- observation{operation: operation, key: key, hit: hit, duration: duration}
	})
	t.Cleanup(func() { SetDebugCacheObserver(nil) })
	start := time.Now()
	observeCache("fallback", "SELECT id FROM owners WHERE token = ?", true, start)
	select {
	case got := <-observed:
		if got.operation != "fallback" || got.key != "owners" || !got.hit || got.duration < 0 {
			t.Fatalf("cache observation = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no cache observation")
	}
}

func TestCacheMetricsObserverReceivesBoundedHitMissAndEvictions(t *testing.T) {
	type event struct {
		cache string
		hit   bool
		count uint64
	}
	events := make(chan event, 3)
	SetCacheMetricsObserver(func(cache string, hit bool) { events <- event{cache: cache, hit: hit} })
	SetCacheEvictionObserver(func(cache string, count uint64) { events <- event{cache: cache, count: count} })
	t.Cleanup(func() {
		SetCacheMetricsObserver(nil)
		SetCacheEvictionObserver(nil)
	})
	start := time.Now()
	observeCache("fallback", "SELECT id FROM owners WHERE token = ?", true, start)
	observeCacheLabel("rate_limit", "external", false, start)
	observeCacheEvictions("rate_limit", 2)
	want := []event{{cache: "remote_db", hit: true}, {cache: "rate_limit", hit: false}, {cache: "rate_limit", count: 2}}
	for _, expected := range want {
		select {
		case got := <-events:
			if got != expected {
				t.Fatalf("cache metric event = %+v, want %+v", got, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("cache metric observer did not receive an event")
		}
	}
}

func TestInMemoryRateLimitCacheProducesHitMissMetrics(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	ConfigureRateLimitCache(nil, "")
	rateLimitCacheWarned.Store(false)
	t.Cleanup(func() {
		ConfigureRateLimitCache(nil, "")
		rateLimitCacheWarned.Store(false)
	})
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	type event struct {
		cache string
		hit   bool
	}
	events := make(chan event, 3)
	SetCacheMetricsObserver(func(cache string, hit bool) { events <- event{cache: cache, hit: hit} })
	t.Cleanup(func() { SetCacheMetricsObserver(nil) })
	key := "cache-metrics-test"
	if _, err := AllowRateLimit(key, 10, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := AllowRateLimit(key, 10, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := RateLimitBlocked(key, 10, 60); err != nil {
		t.Fatal(err)
	}
	want := []event{{cache: "rate_limit", hit: false}, {cache: "rate_limit", hit: true}, {cache: "rate_limit", hit: true}}
	for _, expected := range want {
		select {
		case got := <-events:
			if got != expected {
				t.Fatalf("cache metric event = %+v, want %+v", got, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("in-memory rate-limit cache did not produce a metric")
		}
	}
}

func TestCacheMetricsSnapshotReportsRateLimitSizeAndBytes(t *testing.T) {
	rateLimitFallbackMu.Lock()
	previous := rateLimitFallback
	rateLimitFallback = map[string][]int64{"bounded-test-key": {1, 2, 3}}
	rateLimitFallbackMu.Unlock()
	t.Cleanup(func() {
		rateLimitFallbackMu.Lock()
		rateLimitFallback = previous
		rateLimitFallbackMu.Unlock()
	})
	for _, snapshot := range CacheMetricsSnapshot() {
		if snapshot.Cache == "rate_limit" {
			if snapshot.Size != 1 || snapshot.Bytes != int64(len("bounded-test-key")+3*8) {
				t.Fatalf("rate-limit cache snapshot = %+v", snapshot)
			}
			return
		}
	}
	t.Fatal("rate-limit cache metrics snapshot missing")
}

func TestDebugQueryObserverReceivesStatementAndArgumentCount(t *testing.T) {
	type observation struct {
		query    string
		args     int
		rows     int64
		duration time.Duration
		err      error
	}
	observed := make(chan observation, 1)
	SetDebugQueryObserver(func(query string, args int, rows int64, duration time.Duration, err error) {
		observed <- observation{query: query, args: args, rows: rows, duration: duration, err: err}
	})
	t.Cleanup(func() { SetDebugQueryObserver(nil) })
	start := time.Now()
	want := "SELECT id FROM owners WHERE token = ?"
	observeQuery(want, 1, start, nil)
	select {
	case got := <-observed:
		if got.query != want || got.args != 1 || got.rows != -1 || got.duration < 0 || got.err != nil {
			t.Fatalf("debug query observation = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no debug query observation")
	}
}

type debugTestRows struct{ count, index int }

func (rows *debugTestRows) Columns() []string { return []string{"value"} }
func (rows *debugTestRows) Close() error      { return nil }
func (rows *debugTestRows) Next(destination []driver.Value) error {
	if rows.index >= rows.count {
		return io.EOF
	}
	destination[0] = int64(rows.index)
	rows.index++
	return nil
}

func TestDebugQueryObserverCountsRows(t *testing.T) {
	type observation struct {
		rows int64
		err  error
	}
	observed := make(chan observation, 1)
	SetDebugQueryObserver(func(_ string, _ int, rows int64, _ time.Duration, err error) {
		observed <- observation{rows: rows, err: err}
	})
	t.Cleanup(func() { SetDebugQueryObserver(nil) })
	wrapped := observeDebugRows(&debugTestRows{count: 2}, "SELECT value FROM sample", 0, time.Now())
	buffer := make([]driver.Value, 1)
	for wrapped.Next(buffer) == nil {
	}
	select {
	case got := <-observed:
		if got.rows != 2 || got.err != nil {
			t.Fatalf("query observation = %+v, want 2 successful rows", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no row-count observation")
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
