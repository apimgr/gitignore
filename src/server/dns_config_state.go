package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

// reconcileDNSCredentialState invalidates the prior DNS validation timestamp
// whenever the provider or one of its credentials changes. Only a one-way
// fingerprint is stored in the database.
func reconcileDNSCredentialState(provider, encryptionKey string, credentials map[string]string) error {
	keys := make([]string, 0, len(credentials))
	for key := range credentials {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var input strings.Builder
	input.WriteString(strings.ToLower(strings.TrimSpace(provider)))
	input.WriteByte(0)
	for _, key := range keys {
		input.WriteString(key)
		input.WriteByte(0)
		input.WriteString(credentials[key])
		input.WriteByte(0)
	}
	digest := hmac.New(sha256.New, []byte(encryptionKey))
	_, _ = digest.Write([]byte(input.String()))
	fingerprint := hex.EncodeToString(digest.Sum(nil))
	var previous string
	err := db.Read(context.Background(), func(ctx context.Context, reader *sql.DB) error {
		return reader.QueryRowContext(ctx, "SELECT value FROM app_secrets WHERE name = ?", "dns_config_fingerprint").Scan(&previous)
	})
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && previous == fingerprint {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM app_secrets WHERE name = ?", "dns_validated_at"); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?)
			ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, "dns_config_fingerprint", fingerprint, now)
		return err
	})
}
