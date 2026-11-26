package db

import (
	"context"
	"database/sql"
	"time"
)

type IPBlock struct {
	IP           string
	CIDR         string
	Type         string
	Reason       string
	BlockedAt    time.Time
	ExpiresAt    time.Time
	OffenseCount int
	AutoBlocked  bool
}

func LoadIPBlocks(now time.Time) ([]IPBlock, error) {
	if err := DeleteExpiredIPBlocks(now); err != nil {
		return nil, err
	}
	blocks := make([]IPBlock, 0)
	err := Read(context.Background(), func(ctx context.Context, reader *sql.DB) error {
		rows, err := reader.QueryContext(ctx, `SELECT ip, COALESCE(cidr, ''), type, reason, blocked_at, expires_at, offense_count, auto_blocked FROM ip_blocks WHERE expires_at > ? ORDER BY blocked_at`, now.UTC().Unix())
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var block IPBlock
			var blockedAt, expiresAt int64
			var autoBlocked int
			if err := rows.Scan(&block.IP, &block.CIDR, &block.Type, &block.Reason, &blockedAt, &expiresAt, &block.OffenseCount, &autoBlocked); err != nil {
				return err
			}
			block.BlockedAt = time.Unix(blockedAt, 0).UTC()
			block.ExpiresAt = time.Unix(expiresAt, 0).UTC()
			block.AutoBlocked = autoBlocked != 0
			blocks = append(blocks, block)
		}
		return rows.Err()
	})
	return blocks, err
}

func SaveIPBlock(block IPBlock) error {
	return Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ip_blocks(ip, cidr, type, reason, blocked_at, expires_at, offense_count, auto_blocked)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(ip) DO UPDATE SET cidr=excluded.cidr, type=excluded.type, reason=excluded.reason,
blocked_at=excluded.blocked_at, expires_at=excluded.expires_at, offense_count=excluded.offense_count, auto_blocked=excluded.auto_blocked`,
			block.IP, nullableString(block.CIDR), block.Type, block.Reason, block.BlockedAt.UTC().Unix(), block.ExpiresAt.UTC().Unix(), block.OffenseCount, block.AutoBlocked)
		return err
	})
}

func DeleteIPBlock(ip string) error {
	return Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM ip_blocks WHERE ip = ?", ip)
		return err
	})
}

func DeleteExpiredIPBlocks(now time.Time) error {
	_, err := ExpireIPBlocks(now)
	return err
}

func ExpireIPBlocks(now time.Time) ([]IPBlock, error) {
	expired := make([]IPBlock, 0)
	if err := Read(context.Background(), func(ctx context.Context, reader *sql.DB) error {
		rows, err := reader.QueryContext(ctx, `SELECT ip, COALESCE(cidr, ''), type, reason, blocked_at, expires_at, offense_count, auto_blocked FROM ip_blocks WHERE expires_at <= ?`, now.UTC().Unix())
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var block IPBlock
			var blockedAt, expiresAt int64
			var autoBlocked int
			if err := rows.Scan(&block.IP, &block.CIDR, &block.Type, &block.Reason, &blockedAt, &expiresAt, &block.OffenseCount, &autoBlocked); err != nil {
				return err
			}
			block.BlockedAt = time.Unix(blockedAt, 0).UTC()
			block.ExpiresAt = time.Unix(expiresAt, 0).UTC()
			block.AutoBlocked = autoBlocked != 0
			expired = append(expired, block)
		}
		return rows.Err()
	}); err != nil {
		return expired, err
	}
	if err := Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM ip_blocks WHERE expires_at <= ?", now.UTC().Unix())
		return err
	}); err != nil {
		return expired, err
	}
	return expired, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
