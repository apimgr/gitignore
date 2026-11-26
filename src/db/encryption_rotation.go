package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const encryptionKeyPreviousName = "encryption_key_previous"

// PrepareEncryptionKeyRotation stores the old key wrapped by the new key
// before the caller writes the new key to secrets.yml.
func PrepareEncryptionKeyRotation(oldEncoded, nextEncoded string, now time.Time) error {
	oldKey, err := decodeEncryptionKey(oldEncoded)
	if err != nil {
		return err
	}
	nextKey, err := decodeEncryptionKey(nextEncoded)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		var previousUntil string
		err := tx.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", encryptionKeyPreviousName+"_until").Scan(&previousUntil)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			expires, parseErr := time.Parse(time.RFC3339, previousUntil)
			if parseErr != nil {
				return errors.New("stored encryption key grace period is invalid")
			}
			if now.Before(expires) {
				return errors.New("encryption key rotation is already in progress or within its grace period")
			}
		}
		wrapped, err := encryptAtRest(nextKey, oldKey)
		if err != nil {
			return err
		}
		pending, err := encryptAtRest(oldKey, nextKey)
		if err != nil {
			return err
		}
		nowText := now.UTC().Format(time.RFC3339)
		values := map[string]string{
			encryptionKeyPreviousName:            base64.StdEncoding.EncodeToString(wrapped),
			encryptionKeyPreviousName + "_until": now.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
			"encryption_key_rotation_pending":    base64.StdEncoding.EncodeToString(pending),
		}
		for name, value := range values {
			if _, err := tx.ExecContext(ctx, `INSERT INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?)
				ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, name, value, nowText); err != nil {
				return err
			}
		}
		return nil
	})
}

// CompleteEncryptionKeyRotation eagerly re-encrypts API token copies and every
// AES-encrypted report field. The transaction is all-or-nothing and safe to
// retry after a process interruption.
func CompleteEncryptionKeyRotation(currentEncoded string) error {
	current, err := decodeEncryptionKey(currentEncoded)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	previous, err := previousEncryptionKey(ctx, current)
	if err != nil || previous == nil {
		return err
	}
	lastID := int64(0)
	for {
		nextID, count, err := reencryptOwnerTokenBatch(ctx, previous, current, lastID)
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
		lastID = nextID
	}
	lastTrackingID := ""
	for {
		nextID, count, err := reencryptSecurityReportBatch(ctx, previous, current, lastTrackingID)
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
		lastTrackingID = nextID
	}
	return Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM app_secrets WHERE name = ?", "encryption_key_rotation_pending")
		return err
	})
}

func previousEncryptionKey(ctx context.Context, current []byte) ([]byte, error) {
	mu.RLock()
	reader := conn
	mu.RUnlock()
	if reader == nil {
		return nil, errors.New("database is not initialized")
	}
	var encoded string
	if err := reader.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", encryptionKeyPreviousName).Scan(&encoded); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	wrapped, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("stored previous encryption key is invalid")
	}
	previous, err := decryptAtRest(current, wrapped)
	if err != nil || len(previous) != 32 {
		return nil, errors.New("stored previous encryption key cannot be opened")
	}
	return previous, nil
}

// DecryptOwnerTokenDuringRotation reads a token encrypted with the previous
// key and opportunistically rewrites every matching stored copy with current.
func DecryptOwnerTokenDuringRotation(encoded, currentEncoded string) ([]byte, bool, error) {
	current, err := decodeEncryptionKey(currentEncoded)
	if err != nil {
		return nil, false, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false, err
	}
	if _, err := decryptAtRest(current, ciphertext); err == nil {
		return nil, false, nil
	}
	ctx, cancel := readCtx()
	defer cancel()
	previous, err := previousEncryptionKey(ctx, current)
	if err != nil || previous == nil {
		return nil, false, err
	}
	plaintext, err := decryptAtRest(previous, ciphertext)
	if err != nil {
		return nil, false, err
	}
	reencrypted, err := encryptAtRest(current, plaintext)
	if err != nil {
		return nil, false, err
	}
	writeContext, writeCancel := writeCtx()
	defer writeCancel()
	err = Write(writeContext, func(tx *sql.Tx) error {
		_, updateErr := tx.ExecContext(writeContext, "UPDATE api_tokens SET token_enc = ? WHERE token_enc = ?", base64.StdEncoding.EncodeToString(reencrypted), encoded)
		return updateErr
	})
	if err != nil {
		return nil, false, err
	}
	return plaintext, true, nil
}

// PendingEncryptionKeyRotation recovers a key across a crash between writing
// rotation state and atomically replacing secrets.yml.
func PendingEncryptionKeyRotation(currentEncoded string) (string, bool, error) {
	current, err := decodeEncryptionKey(currentEncoded)
	if err != nil {
		return "", false, err
	}
	mu.RLock()
	reader := readConn
	mu.RUnlock()
	if reader == nil {
		return "", false, errors.New("database is not initialized")
	}
	ctx, cancel := readCtx()
	defer cancel()
	var encoded string
	if err := reader.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", "encryption_key_rotation_pending").Scan(&encoded); err == sql.ErrNoRows {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	pending, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false, errors.New("stored pending encryption key is invalid")
	}
	key, err := decryptAtRest(current, pending)
	if err != nil {
		var wrappedEncoded string
		if queryErr := reader.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", encryptionKeyPreviousName).Scan(&wrappedEncoded); queryErr != nil {
			return "", false, errors.New("stored encryption key rotation cannot be recovered")
		}
		wrapped, decodeErr := base64.StdEncoding.DecodeString(wrappedEncoded)
		if decodeErr != nil {
			return "", false, errors.New("stored previous encryption key is invalid")
		}
		previous, decryptErr := decryptAtRest(current, wrapped)
		if decryptErr != nil {
			return "", false, errors.New("stored encryption key rotation cannot be recovered")
		}
		key, err = decryptAtRest(previous, pending)
	}
	if err != nil || len(key) != 32 {
		return "", false, errors.New("stored pending encryption key cannot be opened")
	}
	encodedKey := base64.StdEncoding.EncodeToString(key)
	return encodedKey, encodedKey != currentEncoded, nil
}

// RetireExpiredSecretRotations removes previous-key material after its grace
// period. Re-encryption runs before a rotation can be marked complete.
func RetireExpiredSecretRotations(now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT name, value FROM app_secrets")
		if err != nil {
			return err
		}
		type expiredSecret struct{ previous, until, value string }
		var expired []expiredSecret
		for rows.Next() {
			var item expiredSecret
			if err := rows.Scan(&item.until, &item.value); err != nil {
				_ = rows.Close()
				return err
			}
			if !strings.HasSuffix(item.until, "_previous_until") {
				continue
			}
			item.previous = strings.TrimSuffix(item.until, "_until")
			deadline, err := time.Parse(time.RFC3339, item.value)
			if err != nil {
				_ = rows.Close()
				return fmt.Errorf("stored secret grace deadline for %s is invalid", item.previous)
			}
			if !now.Before(deadline) {
				expired = append(expired, item)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, item := range expired {
			if _, err := tx.ExecContext(ctx, "DELETE FROM app_secrets WHERE name IN (?, ?)", item.previous, item.until); err != nil {
				return err
			}
		}
		return nil
	})
}

// RotateDueApplicationSecrets rotates browser-signing keys on their documented
// intervals. CSRF keys rotate every 180 days; cookie-signing keys every 90.
func RotateDueApplicationSecrets(now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ctx, cancel := writeCtx()
	defer cancel()
	return Write(ctx, func(tx *sql.Tx) error {
		for _, item := range []struct {
			name     string
			interval time.Duration
			grace    time.Duration
		}{
			{name: "cookie_signing_key", interval: 90 * 24 * time.Hour, grace: 7 * 24 * time.Hour},
			{name: "csrf_token_secret", interval: 180 * 24 * time.Hour},
		} {
			var updated string
			if err := tx.QueryRowContext(ctx, "SELECT COALESCE(updated_at, '') FROM app_secrets WHERE name = ?", item.name).Scan(&updated); err != nil {
				return err
			}
			lastUpdated := time.Time{}
			if updated != "" {
				parsed, err := parseSecretTimestamp(updated)
				if err != nil {
					return fmt.Errorf("stored %s update time is invalid", item.name)
				}
				lastUpdated = parsed
			}
			if now.Sub(lastUpdated) < item.interval {
				continue
			}
			if err := rotateGeneratedAppSecret(ctx, tx, item.name, now, item.grace); err != nil {
				return err
			}
		}
		return nil
	})
}

func rotateGeneratedAppSecret(ctx context.Context, tx *sql.Tx, name string, now time.Time, grace time.Duration) error {
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", name).Scan(&current); err != nil {
		return err
	}
	next := make([]byte, 32)
	if _, err := rand.Read(next); err != nil {
		return err
	}
	nowText := now.UTC().Format(time.RFC3339)
	if grace > 0 {
		for rowName, value := range map[string]string{
			name + "_previous":       current,
			name + "_previous_until": now.Add(grace).UTC().Format(time.RFC3339),
		} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?)
				ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, rowName, value, nowText); err != nil {
				return err
			}
		}
	}
	result, err := tx.ExecContext(ctx, "UPDATE app_secrets SET value = ?, updated_at = ? WHERE name = ? AND value = ?", base64.StdEncoding.EncodeToString(next), nowText, name, current)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s changed during rotation", name)
	}
	return nil
}

func parseSecretTimestamp(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("invalid timestamp")
}

const encryptionRotationBatchSize = 100

func reencryptOwnerTokenBatch(ctx context.Context, previous, current []byte, afterID int64) (int64, int, error) {
	var lastID int64
	count := 0
	err := Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id, token_enc FROM api_tokens WHERE id > ? AND token_enc <> '' ORDER BY id LIMIT ?", afterID, encryptionRotationBatchSize)
		if err != nil {
			return err
		}
		type item struct {
			id  int64
			enc string
		}
		items := make([]item, 0, encryptionRotationBatchSize)
		for rows.Next() {
			var value item
			if err := rows.Scan(&value.id, &value.enc); err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, value)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		count = len(items)
		for _, value := range items {
			lastID = value.id
			ciphertext, err := base64.StdEncoding.DecodeString(value.enc)
			if err != nil {
				return fmt.Errorf("decode encrypted owner token %d", value.id)
			}
			if _, err := decryptAtRest(current, ciphertext); err == nil {
				continue
			}
			plaintext, err := decryptAtRest(previous, ciphertext)
			if err != nil {
				return fmt.Errorf("decrypt encrypted owner token %d", value.id)
			}
			reencrypted, err := encryptAtRest(current, plaintext)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE api_tokens SET token_enc = ? WHERE id = ?", base64.StdEncoding.EncodeToString(reencrypted), value.id); err != nil {
				return err
			}
		}
		return nil
	})
	return lastID, count, err
}

func reencryptSecurityReportBatch(ctx context.Context, previous, current []byte, afterID string) (string, int, error) {
	var lastID string
	count := 0
	err := Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT tracking_id, body_enc, body_alg, contact_enc FROM security_reports WHERE tracking_id > ? ORDER BY tracking_id LIMIT ?", afterID, encryptionRotationBatchSize)
		if err != nil {
			return err
		}
		type item struct {
			id        string
			body      []byte
			algorithm string
			contact   []byte
		}
		items := make([]item, 0, encryptionRotationBatchSize)
		for rows.Next() {
			var value item
			if err := rows.Scan(&value.id, &value.body, &value.algorithm, &value.contact); err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, value)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		count = len(items)
		for _, value := range items {
			lastID = value.id
			var body, contact []byte
			if value.algorithm == "aes-256-gcm" && len(value.body) > 0 {
				body, err = rotateCiphertext(previous, current, value.body)
				if err != nil {
					return fmt.Errorf("reencrypt security report %s", value.id)
				}
			}
			if len(value.contact) > 0 {
				contact, err = rotateCiphertext(previous, current, value.contact)
				if err != nil {
					return fmt.Errorf("reencrypt security report contact %s", value.id)
				}
			}
			if body == nil && contact == nil {
				continue
			}
			if body == nil {
				_, err = tx.ExecContext(ctx, "UPDATE security_reports SET contact_enc = ? WHERE tracking_id = ?", contact, value.id)
			} else if contact == nil {
				_, err = tx.ExecContext(ctx, "UPDATE security_reports SET body_enc = ? WHERE tracking_id = ?", body, value.id)
			} else {
				_, err = tx.ExecContext(ctx, "UPDATE security_reports SET body_enc = ?, contact_enc = ? WHERE tracking_id = ?", body, contact, value.id)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return lastID, count, err
}

func rotateCiphertext(previous, current, ciphertext []byte) ([]byte, error) {
	if _, err := decryptAtRest(current, ciphertext); err == nil {
		return nil, nil
	}
	plaintext, err := decryptAtRest(previous, ciphertext)
	if err != nil {
		return nil, err
	}
	return encryptAtRest(current, plaintext)
}

func decodeEncryptionKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(key) != 32 {
		return nil, errors.New("encryption key must be base64-encoded 32-byte key material")
	}
	return key, nil
}

func encryptAtRest(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plaintext, nil)...), nil
}

func decryptAtRest(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("encrypted data is truncated")
	}
	nonce := ciphertext[:aead.NonceSize()]
	return aead.Open(nil, nonce, ciphertext[aead.NonceSize():], nil)
}
