package db

import (
	"database/sql"
	"time"
)

// SchedulerState is the persisted execution state of a single scheduled task
// (AI.md PART 18 "Scheduler State (Persistent)"). Times are stored as RFC3339
// UTC text so they round-trip identically across SQLite driver versions.
type SchedulerState struct {
	TaskID     string
	TaskName   string
	Schedule   string
	LastRun    time.Time
	LastStatus string
	LastError  string
	NextRun    time.Time
	RunCount   int64
	FailCount  int64
	Enabled    bool
}

// LoadSchedulerStates returns every persisted task state, keyed insertion order.
func LoadSchedulerStates() ([]SchedulerState, error) {
	mu.RLock()
	defer mu.RUnlock()

	ctx, cancel := readCtx()
	defer cancel()

	rows, err := conn.QueryContext(ctx, `
SELECT task_id, task_name, schedule, last_run, last_status, last_error,
       next_run, run_count, fail_count, enabled
FROM server_scheduler_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []SchedulerState
	for rows.Next() {
		st, err := scanSchedulerState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	return states, rows.Err()
}

// GetSchedulerState returns the persisted state for one task, or nil if absent.
func GetSchedulerState(taskID string) (*SchedulerState, error) {
	mu.RLock()
	defer mu.RUnlock()

	ctx, cancel := readCtx()
	defer cancel()

	row := conn.QueryRowContext(ctx, `
SELECT task_id, task_name, schedule, last_run, last_status, last_error,
       next_run, run_count, fail_count, enabled
FROM server_scheduler_state WHERE task_id = ?`, taskID)

	st, err := scanSchedulerState(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// UpsertSchedulerState writes the full task state, inserting or replacing.
func UpsertSchedulerState(st SchedulerState) error {
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := writeCtx()
	defer cancel()

	_, err := conn.ExecContext(ctx, `
INSERT INTO server_scheduler_state
    (task_id, task_name, schedule, last_run, last_status, last_error,
     next_run, run_count, fail_count, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
    task_name   = excluded.task_name,
    schedule    = excluded.schedule,
    last_run    = excluded.last_run,
    last_status = excluded.last_status,
    last_error  = excluded.last_error,
    next_run    = excluded.next_run,
    run_count   = excluded.run_count,
    fail_count  = excluded.fail_count,
    enabled     = excluded.enabled`,
		st.TaskID, st.TaskName, st.Schedule,
		nullTimeText(st.LastRun), st.LastStatus, st.LastError,
		nullTimeText(st.NextRun), st.RunCount, st.FailCount, boolToInt(st.Enabled))
	return err
}

// SetSchedulerEnabled toggles the enabled flag for one task without touching
// its counters or timestamps. Returns false if the task row does not exist.
func SetSchedulerEnabled(taskID string, enabled bool) (bool, error) {
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := writeCtx()
	defer cancel()

	res, err := conn.ExecContext(ctx,
		"UPDATE server_scheduler_state SET enabled = ? WHERE task_id = ?",
		boolToInt(enabled), taskID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CleanupExpiredTokens removes expired API tokens and sessions across all token
// tables and returns the total number of rows deleted (AI.md PART 18
// token_cleanup task).
func CleanupExpiredTokens() (int64, error) {
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := writeCtx()
	defer cancel()

	var total int64
	stmts := []string{
		"DELETE FROM user_tokens WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP",
		"DELETE FROM user_sessions WHERE expires_at <= CURRENT_TIMESTAMP",
		"DELETE FROM server_admin_sessions WHERE expires_at <= CURRENT_TIMESTAMP",
		"DELETE FROM server_join_tokens WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP",
		"DELETE FROM user_invites WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP AND used_by IS NULL",
	}
	for _, q := range stmts {
		res, err := conn.ExecContext(ctx, q)
		if err != nil {
			return total, err
		}
		if n, err := res.RowsAffected(); err == nil {
			total += n
		}
	}
	return total, nil
}

// Ping verifies the database connection is reachable (AI.md PART 18
// healthcheck_self task).
func Ping() error {
	mu.RLock()
	defer mu.RUnlock()

	ctx, cancel := readCtx()
	defer cancel()
	return conn.PingContext(ctx)
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSchedulerState(s scanner) (SchedulerState, error) {
	var (
		st                SchedulerState
		lastRun, nextRun  sql.NullString
		enabled           int
	)
	if err := s.Scan(&st.TaskID, &st.TaskName, &st.Schedule, &lastRun,
		&st.LastStatus, &st.LastError, &nextRun, &st.RunCount, &st.FailCount,
		&enabled); err != nil {
		return st, err
	}
	st.LastRun = parseTimeText(lastRun)
	st.NextRun = parseTimeText(nextRun)
	st.Enabled = enabled != 0
	return st, nil
}

// nullTimeText renders a time as RFC3339 UTC text, or NULL for the zero value.
func nullTimeText(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// parseTimeText parses stored time text, tolerating both RFC3339 (our format)
// and SQLite's default "2006-01-02 15:04:05" form.
func parseTimeText(v sql.NullString) time.Time {
	if !v.Valid || v.String == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v.String); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
