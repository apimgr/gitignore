package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RequestStats is the persisted aggregate HTTP request count and hourly
// buckets used by the public health endpoint.
type RequestStats struct {
	Total  int64
	ByHour map[int64]int64
}

// LoadRequestStats reads the persisted request counters and hourly buckets.
func LoadRequestStats(ctx context.Context) (RequestStats, error) {
	stats := RequestStats{ByHour: make(map[int64]int64)}
	err := Read(ctx, func(queryCtx context.Context, reader *sql.DB) error {
		if err := reader.QueryRowContext(queryCtx, "SELECT value FROM server_stats WHERE name = ?", "requests_total").Scan(&stats.Total); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rows, err := reader.QueryContext(queryCtx, "SELECT hour, count FROM server_stat_hours")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var hour, count int64
			if err := rows.Scan(&hour, &count); err != nil {
				return err
			}
			stats.ByHour[hour] = count
		}
		return errors.Join(rows.Err(), rows.Close())
	})
	if err != nil {
		return RequestStats{}, err
	}
	if stats.Total < 0 {
		return RequestStats{}, fmt.Errorf("stored request total is negative")
	}
	for _, count := range stats.ByHour {
		if count < 0 {
			return RequestStats{}, fmt.Errorf("stored hourly request count is negative")
		}
	}
	return stats, nil
}

// SaveRequestStats atomically stores the lifetime total and current 24-hour
// buckets. The caller batches calls at no more than 60-second intervals.
func SaveRequestStats(ctx context.Context, stats RequestStats, currentHour int64) error {
	if stats.Total < 0 {
		return fmt.Errorf("request total cannot be negative")
	}
	for hour, count := range stats.ByHour {
		if count < 0 || hour > currentHour || hour < currentHour-23 {
			return fmt.Errorf("invalid request hour bucket")
		}
	}
	return Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO server_stats(name, value) VALUES(?, ?)
			ON CONFLICT(name) DO UPDATE SET value = excluded.value`, "requests_total", stats.Total); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM server_stat_hours WHERE hour < ? OR hour > ?", currentHour-23, currentHour); err != nil {
			return err
		}
		for hour, count := range stats.ByHour {
			if _, err := tx.ExecContext(ctx, `INSERT INTO server_stat_hours(hour, count) VALUES(?, ?)
				ON CONFLICT(hour) DO UPDATE SET count = excluded.count`, hour, count); err != nil {
				return err
			}
		}
		return nil
	})
}
