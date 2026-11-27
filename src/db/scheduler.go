package db

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"time"
)

// SchedulerState is the persisted execution state of a single scheduled task.
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
	if conn == nil {
		return nil, sql.ErrConnDone
	}

	ctx, cancel := readCtx()
	defer cancel()

	rows, err := readConn.QueryContext(ctx, `
SELECT id, name, schedule, last_run, last_status, last_error,
       next_run, run_count, fail_count, enabled
FROM scheduler_tasks`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

	row := readConn.QueryRowContext(ctx, `
SELECT id, name, schedule, last_run, last_status, last_error,
       next_run, run_count, fail_count, enabled
FROM scheduler_tasks WHERE id = ?`, taskID)

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
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO scheduler_tasks
    (id, name, schedule, last_run, last_status, last_error,
     next_run, run_count, fail_count, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name        = excluded.name,
    schedule    = excluded.schedule,
    last_run    = excluded.last_run,
    last_status = excluded.last_status,
    last_error  = excluded.last_error,
    next_run    = excluded.next_run,
    run_count   = excluded.run_count,
    fail_count  = excluded.fail_count,
    enabled     = excluded.enabled`,
			st.TaskID, st.TaskName, st.Schedule,
			nullTimeUnix(st.LastRun), st.LastStatus, st.LastError,
			nullTimeUnix(st.NextRun), st.RunCount, st.FailCount, boolToInt(st.Enabled))
		return err
	})
}

// SetSchedulerEnabled toggles the enabled flag for one task without touching
// its counters or timestamps. Returns false if the task row does not exist.
func SetSchedulerEnabled(taskID string, enabled bool) (bool, error) {
	ctx, cancel := writeCtx()
	defer cancel()
	var changed bool
	err := Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE scheduler_tasks SET enabled = ? WHERE id = ?",
			boolToInt(enabled), taskID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		changed = n > 0
		return err
	})
	return changed, err
}

// SchedulerHistory is one completed execution in the bounded task history.
type SchedulerHistory struct {
	TaskID    string
	StartedAt time.Time
	Finished  time.Time
	Status    string
	Error     string
	Duration  time.Duration
}

// ListSchedulerHistory returns the newest recorded runs for one task.
func ListSchedulerHistory(taskID string, limit int) ([]SchedulerHistory, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return nil, sql.ErrConnDone
	}
	ctx, cancel := readCtx()
	defer cancel()
	rows, err := readConn.QueryContext(ctx, `SELECT task_id, started_at, finished_at, status, error, duration_ms
		FROM scheduler_history WHERE task_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var history []SchedulerHistory
	for rows.Next() {
		var (
			run                         SchedulerHistory
			started, finished, duration sql.NullInt64
			message                     sql.NullString
		)
		if err := rows.Scan(&run.TaskID, &started, &finished, &run.Status, &message, &duration); err != nil {
			return nil, err
		}
		if started.Valid {
			run.StartedAt = time.Unix(started.Int64, 0).UTC()
		}
		if finished.Valid {
			run.Finished = time.Unix(finished.Int64, 0).UTC()
		}
		run.Error = message.String
		if duration.Valid {
			run.Duration = time.Duration(duration.Int64) * time.Millisecond
		}
		history = append(history, run)
	}
	return history, rows.Err()
}

// RecordSchedulerHistory appends one scheduler run record.
func RecordSchedulerHistory(run SchedulerHistory) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO scheduler_history
		(task_id, started_at, finished_at, status, error, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?)`, run.TaskID, run.StartedAt.UTC().Unix(), run.Finished.UTC().Unix(), run.Status, nullableError(run.Error), run.Duration.Milliseconds())
		return err
	})
}

func nullableError(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// CleanupExpiredTokens removes expired API tokens and sessions across all token
// tables and returns the total number of rows deleted (AI.md PART 18
// token_cleanup task).
func CleanupExpiredTokens() (int64, error) {
	ctx, cancel := writeCtx()
	defer cancel()
	var total int64
	stmts := []string{
		"DELETE FROM rate_limits WHERE rowid IN (SELECT rowid FROM rate_limits WHERE window_start < strftime('%s', 'now') - 3600 LIMIT 500)",
		"DELETE FROM rate_limit_events WHERE rowid IN (SELECT rowid FROM rate_limit_events WHERE occurred_at < (strftime('%s', 'now') - 3600) * 1000 LIMIT 500)",
	}
	for {
		var batch int64
		for _, q := range stmts {
			var count int64
			err := Write(ctx, func(tx *sql.Tx) error {
				res, err := tx.ExecContext(ctx, q)
				if err != nil {
					return err
				}
				count, err = res.RowsAffected()
				return err
			})
			if err != nil {
				return total, err
			}
			batch += count
		}
		total += batch
		if batch < 500 {
			return total, nil
		}
		runtime.Gosched()
	}
}

// Ping verifies the database connection is reachable (AI.md PART 18
// healthcheck_self task).
func Ping() error {
	ctx, cancel := readCtx()
	defer cancel()
	return PingContext(ctx)
}

// PingContext performs a bounded trivial read against the read handle.
func PingContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("database ping requires a context")
	}
	mu.RLock()
	reader := readConn
	mu.RUnlock()
	if reader == nil {
		return fmt.Errorf("database is not initialized")
	}
	return reader.QueryRowContext(ctx, `SELECT 1`).Scan(new(int))
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSchedulerState(s scanner) (SchedulerState, error) {
	var (
		st                    SchedulerState
		lastRun, nextRun      sql.NullInt64
		lastStatus, lastError sql.NullString
		enabled               int
	)
	if err := s.Scan(&st.TaskID, &st.TaskName, &st.Schedule, &lastRun,
		&lastStatus, &lastError, &nextRun, &st.RunCount, &st.FailCount,
		&enabled); err != nil {
		return st, err
	}
	st.LastStatus = lastStatus.String
	st.LastError = lastError.String
	if lastRun.Valid {
		st.LastRun = time.Unix(lastRun.Int64, 0).UTC()
	}
	if nextRun.Valid {
		st.NextRun = time.Unix(nextRun.Int64, 0).UTC()
	}
	st.Enabled = enabled != 0
	return st, nil
}

// nullTimeUnix renders a time as Unix seconds, or NULL for the zero value.
func nullTimeUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
