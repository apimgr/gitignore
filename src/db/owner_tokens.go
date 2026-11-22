package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrOwnerTokenNotFound = errors.New("owner token not found")

type OwnerToken struct {
	ID            int64
	OwnerID       string
	Kind          string
	Name          sql.NullString
	TokenHash     string
	TokenPrefix   string
	TokenEnc      string
	CreatedAt     int64
	RotatedAt     sql.NullInt64
	ExpiresAt     sql.NullInt64
	LastUsedAt    sql.NullInt64
	RevokedAt     sql.NullInt64
	RevokedReason sql.NullString
}

func ownerTokenDB() (*sql.DB, error) {
	mu.RLock()
	defer mu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	return conn, nil
}

// ActiveOwnerTokenCounts returns non-revoked, unexpired primary and linked
// token counts for the owner-token metrics.
func ActiveOwnerTokenCounts(now time.Time) (primary, linked int, err error) {
	c, err := ownerTokenDB()
	if err != nil {
		return 0, 0, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	rows, err := c.QueryContext(ctx, `SELECT kind, COUNT(*) FROM api_tokens
WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) GROUP BY kind`, now.UTC().Unix())
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return 0, 0, err
		}
		switch kind {
		case "primary":
			primary = count
		case "linked":
			linked = count
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return primary, linked, nil
}

func CreateOwnerWithPrimary(ownerID, tokenHash, tokenPrefix, tokenEnc string, now time.Time) (OwnerToken, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return OwnerToken{}, err
	}
	ctx, cancel := writeCtx()
	defer cancel()
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return OwnerToken{}, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now.UTC().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO owners(id, created_at, last_seen_at) VALUES(?, ?, ?)`, ownerID, stamp, stamp); err != nil {
		return OwnerToken{}, fmt.Errorf("create owner: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO api_tokens(owner_id, kind, token_hash, token_prefix, token_enc, created_at)
VALUES(?, 'primary', ?, ?, ?, ?)`, ownerID, tokenHash, tokenPrefix, tokenEnc, stamp)
	if err != nil {
		return OwnerToken{}, fmt.Errorf("create primary owner token: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return OwnerToken{}, err
	}
	if err := tx.Commit(); err != nil {
		return OwnerToken{}, err
	}
	return OwnerToken{ID: id, OwnerID: ownerID, Kind: "primary", TokenHash: tokenHash, TokenPrefix: tokenPrefix, TokenEnc: tokenEnc, CreatedAt: stamp}, nil
}

func FindOwnerTokensByPrefix(prefix string) ([]OwnerToken, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	rows, err := c.QueryContext(ctx, `SELECT id, owner_id, kind, name, token_hash, token_prefix, token_enc, created_at,
rotated_at, expires_at, last_used_at, revoked_at, revoked_reason
FROM api_tokens WHERE token_prefix = ? ORDER BY id`, prefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]OwnerToken, 0)
	for rows.Next() {
		var item OwnerToken
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Kind, &item.Name, &item.TokenHash, &item.TokenPrefix, &item.TokenEnc,
			&item.CreatedAt, &item.RotatedAt, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt, &item.RevokedReason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func MarkOwnerTokenUsed(tokenID int64, now time.Time) error {
	c, err := ownerTokenDB()
	if err != nil {
		return err
	}
	ctx, cancel := writeCtx()
	defer cancel()
	stamp := now.UTC().Unix()
	result, err := c.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ? AND revoked_at IS NULL
AND (expires_at IS NULL OR expires_at > ?) AND (last_used_at IS NULL OR last_used_at <= ?)`, stamp, tokenID, stamp, stamp-60)
	if err != nil {
		return err
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, `UPDATE owners SET last_seen_at = ? WHERE id = (SELECT owner_id FROM api_tokens WHERE id = ?)`, stamp, tokenID)
	return err
}

func ListOwnerTokens(ownerID string) ([]OwnerToken, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	rows, err := c.QueryContext(ctx, `SELECT id, owner_id, kind, name, token_hash, token_prefix, token_enc, created_at,
rotated_at, expires_at, last_used_at, revoked_at, revoked_reason FROM api_tokens
WHERE owner_id = ? ORDER BY CASE kind WHEN 'primary' THEN 0 ELSE 1 END, created_at DESC, id DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]OwnerToken, 0)
	for rows.Next() {
		var item OwnerToken
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Kind, &item.Name, &item.TokenHash, &item.TokenPrefix, &item.TokenEnc,
			&item.CreatedAt, &item.RotatedAt, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt, &item.RevokedReason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func GetOwnerToken(ownerID string, tokenID int64) (OwnerToken, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return OwnerToken{}, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	var item OwnerToken
	err = c.QueryRowContext(ctx, `SELECT id, owner_id, kind, name, token_hash, token_prefix, token_enc, created_at,
rotated_at, expires_at, last_used_at, revoked_at, revoked_reason FROM api_tokens WHERE owner_id = ? AND id = ?`, ownerID, tokenID).
		Scan(&item.ID, &item.OwnerID, &item.Kind, &item.Name, &item.TokenHash, &item.TokenPrefix, &item.TokenEnc,
			&item.CreatedAt, &item.RotatedAt, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt, &item.RevokedReason)
	if errors.Is(err, sql.ErrNoRows) {
		return OwnerToken{}, ErrOwnerTokenNotFound
	}
	return item, err
}

func CountActiveLinkedTokens(ownerID string, now time.Time) (int, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return 0, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	var count int
	err = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ? AND kind = 'linked'
AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, ownerID, now.UTC().Unix()).Scan(&count)
	return count, err
}

// PurgeIdleOwners removes owners with no owned resources whose last valid token
// use predates cutoff. This catalog has no user-created resource rows, so every
// owner is resource-free; the foreign key cascade removes its tokens.
func PurgeIdleOwners(ctx context.Context, cutoff time.Time) (int64, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return 0, err
	}
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `DELETE FROM owners WHERE last_seen_at < ?`, cutoff.UTC().Unix())
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func CreateLinkedOwnerToken(ownerID, name, tokenHash, tokenPrefix, tokenEnc string, expiresAt *int64, maxLinked int, now time.Time) (OwnerToken, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return OwnerToken{}, err
	}
	ctx, cancel := writeCtx()
	defer cancel()
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return OwnerToken{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ? AND kind = 'linked'
AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, ownerID, now.UTC().Unix()).Scan(&active); err != nil {
		return OwnerToken{}, err
	}
	if maxLinked > 0 && active >= maxLinked {
		return OwnerToken{}, errors.New("maximum active linked token count reached")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO api_tokens(owner_id, kind, name, token_hash, token_prefix, token_enc, created_at, expires_at)
SELECT ?, 'linked', ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM owners WHERE id = ?)`,
		ownerID, name, tokenHash, tokenPrefix, tokenEnc, now.UTC().Unix(), expiresAt, ownerID)
	if err != nil {
		return OwnerToken{}, fmt.Errorf("create linked owner token: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return OwnerToken{}, err
	}
	if changed != 1 {
		return OwnerToken{}, ErrOwnerTokenNotFound
	}
	id, err := result.LastInsertId()
	if err != nil {
		return OwnerToken{}, err
	}
	if err := tx.Commit(); err != nil {
		return OwnerToken{}, err
	}
	item := OwnerToken{ID: id, OwnerID: ownerID, Kind: "linked", Name: sql.NullString{String: name, Valid: true}, TokenHash: tokenHash, TokenPrefix: tokenPrefix, TokenEnc: tokenEnc, CreatedAt: now.UTC().Unix()}
	if expiresAt != nil {
		item.ExpiresAt = sql.NullInt64{Int64: *expiresAt, Valid: true}
	}
	return item, nil
}

func RotateOwnerToken(ownerID string, tokenID int64, tokenHash, tokenPrefix, tokenEnc string, now time.Time) error {
	c, err := ownerTokenDB()
	if err != nil {
		return err
	}
	ctx, cancel := writeCtx()
	defer cancel()
	result, err := c.ExecContext(ctx, `UPDATE api_tokens SET token_hash = ?, token_prefix = ?, token_enc = ?, rotated_at = ?, revoked_at = NULL, revoked_reason = NULL
WHERE owner_id = ? AND id = ? AND revoked_at IS NULL`, tokenHash, tokenPrefix, tokenEnc, now.UTC().Unix(), ownerID, tokenID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrOwnerTokenNotFound
	}
	return nil
}

func UpdateLinkedOwnerToken(ownerID string, tokenID int64, name string, expiresAt *int64) error {
	c, err := ownerTokenDB()
	if err != nil {
		return err
	}
	ctx, cancel := writeCtx()
	defer cancel()
	result, err := c.ExecContext(ctx, `UPDATE api_tokens SET name = ?, expires_at = ?
WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, name, expiresAt, ownerID, tokenID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrOwnerTokenNotFound
	}
	return nil
}

func RevokeLinkedOwnerToken(ownerID string, tokenID int64, now time.Time) error {
	c, err := ownerTokenDB()
	if err != nil {
		return err
	}
	ctx, cancel := writeCtx()
	defer cancel()
	result, err := c.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ?, revoked_reason = 'owner_revoked'
WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, now.UTC().Unix(), ownerID, tokenID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrOwnerTokenNotFound
	}
	return nil
}

func RevokeOwnerTokenPrefix(prefix string, now time.Time) (int64, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var ownerID, kind string
	if err := tx.QueryRowContext(ctx, `SELECT owner_id, kind FROM api_tokens WHERE token_prefix = ? AND revoked_at IS NULL ORDER BY id LIMIT 1`, prefix).Scan(&ownerID, &kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrOwnerTokenNotFound
		}
		return 0, err
	}
	var result sql.Result
	if kind == "primary" {
		result, err = tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ?, revoked_reason = 'operator_action' WHERE owner_id = ? AND revoked_at IS NULL`, now.UTC().Unix(), ownerID)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ?, revoked_reason = 'operator_action' WHERE token_prefix = ? AND revoked_at IS NULL`, now.UTC().Unix(), prefix)
	}
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}
