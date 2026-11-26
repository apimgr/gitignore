package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
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

type OwnerRecord struct {
	ID         string
	CreatedAt  int64
	LastSeenAt int64
}

func OwnerTokenLast4(ownerID string) (string, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return "", err
	}
	ctx, cancel := readCtx()
	defer cancel()
	var value string
	err = c.QueryRowContext(ctx, `SELECT token_last4 FROM api_tokens WHERE owner_id = ? AND kind = 'primary' AND revoked_at IS NULL`, ownerID).Scan(&value)
	return value, err
}

func SetOwnerTokenLast4IfEmpty(tokenID int64, value string) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE api_tokens SET token_last4 = ? WHERE id = ? AND token_last4 = ''`, value, tokenID)
		return err
	})
}

func GetOwner(ownerID string) (OwnerRecord, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return OwnerRecord{}, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	var owner OwnerRecord
	err = c.QueryRowContext(ctx, `SELECT id, created_at, last_seen_at FROM owners WHERE id = ?`, ownerID).
		Scan(&owner.ID, &owner.CreatedAt, &owner.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OwnerRecord{}, ErrOwnerTokenNotFound
	}
	return owner, err
}

func ListAllOwnerTokens() ([]OwnerToken, error) {
	c, err := ownerTokenDB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	rows, err := c.QueryContext(ctx, `SELECT id, owner_id, kind, name, token_hash, token_prefix, token_enc, created_at,
rotated_at, expires_at, last_used_at, revoked_at, revoked_reason FROM api_tokens ORDER BY owner_id, id`)
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

func OwnerAndActiveTokenCounts(now time.Time) (owners, tokens int, err error) {
	c, err := ownerTokenDB()
	if err != nil {
		return 0, 0, err
	}
	ctx, cancel := readCtx()
	defer cancel()
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM owners`).Scan(&owners); err != nil {
		return 0, 0, err
	}
	err = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, now.UTC().Unix()).Scan(&tokens)
	return owners, tokens, err
}

func DeleteOwner(ownerID string) (tokensDeleted int64, err error) {
	ctx, cancel := writeCtx()
	defer cancel()
	err = Write(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ?`, ownerID).Scan(&tokensDeleted); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM owners WHERE id = ?`, ownerID)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if deleted != 1 {
			return ErrOwnerTokenNotFound
		}
		return nil
	})
	return tokensDeleted, err
}

func ownerTokenDB() (*sql.DB, error) {
	mu.RLock()
	defer mu.RUnlock()
	if readConn == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	return readConn, nil
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

func CreateOwnerWithPrimary(ownerID, tokenHash, tokenPrefix, tokenLast4, tokenEnc string, now time.Time) (OwnerToken, error) {
	ctx, cancel := writeCtx()
	defer cancel()
	stamp := now.UTC().Unix()
	var id int64
	err := Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO owners(id, created_at, last_seen_at) VALUES(?, ?, ?)`, ownerID, stamp, stamp); err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO api_tokens(owner_id, kind, token_hash, token_prefix, token_last4, token_enc, created_at)
VALUES(?, 'primary', ?, ?, ?, ?, ?)`, ownerID, tokenHash, tokenPrefix, tokenLast4, tokenEnc, stamp)
		if err != nil {
			return fmt.Errorf("create primary owner token: %w", err)
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
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
	return queueOwnerTokenUse(tokenID, now)
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
	var total int64
	for {
		var count int64
		err := Write(ctx, func(tx *sql.Tx) error {
			result, err := tx.ExecContext(ctx, `DELETE FROM owners WHERE id IN (SELECT id FROM owners WHERE last_seen_at < ? LIMIT 500)`, cutoff.UTC().Unix())
			if err != nil {
				return err
			}
			count, err = result.RowsAffected()
			return err
		})
		if err != nil {
			return total, err
		}
		total += count
		if count < 500 {
			return total, nil
		}
		runtime.Gosched()
	}
}

func CreateLinkedOwnerToken(ownerID, name, tokenHash, tokenPrefix, tokenLast4, tokenEnc string, expiresAt *int64, maxLinked int, now time.Time) (OwnerToken, error) {
	ctx, cancel := writeCtx()
	defer cancel()
	var active int
	var id int64
	err := Write(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ? AND kind = 'linked'
AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, ownerID, now.UTC().Unix()).Scan(&active); err != nil {
			return err
		}
		if maxLinked > 0 && active >= maxLinked {
			return errors.New("maximum active linked token count reached")
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO api_tokens(owner_id, kind, name, token_hash, token_prefix, token_last4, token_enc, created_at, expires_at)
SELECT ?, 'linked', ?, ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM owners WHERE id = ?)`,
			ownerID, name, tokenHash, tokenPrefix, tokenLast4, tokenEnc, now.UTC().Unix(), expiresAt, ownerID)
		if err != nil {
			return fmt.Errorf("create linked owner token: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrOwnerTokenNotFound
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return OwnerToken{}, err
	}
	item := OwnerToken{ID: id, OwnerID: ownerID, Kind: "linked", Name: sql.NullString{String: name, Valid: true}, TokenHash: tokenHash, TokenPrefix: tokenPrefix, TokenEnc: tokenEnc, CreatedAt: now.UTC().Unix()}
	if expiresAt != nil {
		item.ExpiresAt = sql.NullInt64{Int64: *expiresAt, Valid: true}
	}
	return item, nil
}

func RotateOwnerToken(ownerID string, tokenID int64, tokenHash, tokenPrefix, tokenLast4, tokenEnc string, now time.Time) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE api_tokens SET token_hash = ?, token_prefix = ?, token_last4 = ?, token_enc = ?, rotated_at = ?, revoked_at = NULL, revoked_reason = NULL
WHERE owner_id = ? AND id = ? AND revoked_at IS NULL`, tokenHash, tokenPrefix, tokenLast4, tokenEnc, now.UTC().Unix(), ownerID, tokenID)
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
	})
}

func UpdateLinkedOwnerToken(ownerID string, tokenID int64, name string, expiresAt *int64) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE api_tokens SET name = ?, expires_at = ?
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
	})
}

func RevokeLinkedOwnerToken(ownerID string, tokenID int64, now time.Time) error {
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ?, revoked_reason = 'owner_revoked'
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
	})
}

func RevokeOwnerTokenPrefix(prefix string, now time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	var deletedCount int64
	err := Write(ctx, func(tx *sql.Tx) error {
		var ownerID, kind string
		if err := tx.QueryRowContext(ctx, `SELECT owner_id, kind FROM api_tokens WHERE token_prefix = ? AND revoked_at IS NULL ORDER BY id LIMIT 1`, prefix).Scan(&ownerID, &kind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrOwnerTokenNotFound
			}
			return err
		}
		var result sql.Result
		var err error
		if kind == "primary" {
			var tokenCount int64
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ?`, ownerID).Scan(&tokenCount); err != nil {
				return err
			}
			result, err = tx.ExecContext(ctx, `DELETE FROM owners WHERE id = ?`, ownerID)
			if err != nil {
				return err
			}
			deleted, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return rowsErr
			}
			if deleted != 1 {
				return ErrOwnerTokenNotFound
			}
			deletedCount = tokenCount
			return nil
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ?, revoked_reason = 'operator_action' WHERE token_prefix = ? AND revoked_at IS NULL`, now.UTC().Unix(), prefix)
		}
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		deletedCount = changed
		return nil
	})
	return deletedCount, err
}
