package db

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

var (
	conn   *sql.DB
	mu     sync.RWMutex
)

// AdminCredentials holds admin login info loaded from DB
type AdminCredentials struct {
	Username  string
	PassHash  string // Argon2id PHC string
	TokenHash string // SHA-256 hex of the raw token
}

// Init opens (or creates) the SQLite database and runs schema migrations
func Init(dataDir string) error {
	mu.Lock()
	defer mu.Unlock()

	dbDir := filepath.Join(dataDir, "db")
	if err := os.MkdirAll(dbDir, 0750); err != nil {
		return fmt.Errorf("failed to create db directory: %w", err)
	}

	path := filepath.Join(dbDir, "gitignore.db")
	c, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := c.Exec("PRAGMA journal_mode=WAL"); err != nil {
		c.Close()
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := c.Exec("PRAGMA foreign_keys=ON"); err != nil {
		c.Close()
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if err := createSchema(c); err != nil {
		c.Close()
		return fmt.Errorf("failed to create schema: %w", err)
	}

	conn = c
	return nil
}

// Close closes the database connection
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func createSchema(c *sql.DB) error {
	_, err := c.Exec(`
CREATE TABLE IF NOT EXISTS server_admin_credentials (
    id          INTEGER PRIMARY KEY,
    username    TEXT NOT NULL,
    pass_hash   TEXT NOT NULL,
    token_hash  TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS server_admin_sessions (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL,
    ip          TEXT
);

CREATE TABLE IF NOT EXISTS server_config (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_by  TEXT DEFAULT 'admin'
);

CREATE TABLE IF NOT EXISTS server_cluster_state (
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    node_id     TEXT NOT NULL DEFAULT '',
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (key, node_id)
);

CREATE TABLE IF NOT EXISTS server_scheduler_state (
    task        TEXT PRIMARY KEY,
    last_run    DATETIME,
    next_run    DATETIME,
    status      TEXT
);

CREATE TABLE IF NOT EXISTS server_nodes (
    id          TEXT PRIMARY KEY,
    address     TEXT NOT NULL,
    joined_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_seen   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS server_join_tokens (
    token_hash  TEXT PRIMARY KEY,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME
);

CREATE TABLE IF NOT EXISTS user_accounts (
    id          TEXT PRIMARY KEY,
    username    TEXT UNIQUE NOT NULL,
    pass_hash   TEXT NOT NULL,
    email       TEXT UNIQUE,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES user_accounts(id),
    token_hash  TEXT NOT NULL,
    name        TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME
);

CREATE TABLE IF NOT EXISTS user_sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES user_accounts(id),
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL,
    ip          TEXT
);

CREATE TABLE IF NOT EXISTS user_invites (
    code        TEXT PRIMARY KEY,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME,
    used_by     TEXT
);
`)
	return err
}

// HasAdminCredentials returns true if admin credentials have been set
func HasAdminCredentials() (bool, error) {
	mu.RLock()
	defer mu.RUnlock()

	var count int
	err := conn.QueryRow("SELECT COUNT(*) FROM server_admin_credentials").Scan(&count)
	return count > 0, err
}

// GetAdminCredentials returns stored admin credentials
func GetAdminCredentials() (*AdminCredentials, error) {
	mu.RLock()
	defer mu.RUnlock()

	creds := &AdminCredentials{}
	err := conn.QueryRow(
		"SELECT username, pass_hash, token_hash FROM server_admin_credentials ORDER BY id LIMIT 1",
	).Scan(&creds.Username, &creds.PassHash, &creds.TokenHash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return creds, err
}

// SetAdminCredentials stores admin credentials. password is hashed with Argon2id;
// token is hashed with SHA-256 before storage.
func SetAdminCredentials(username, password, token string) error {
	passHash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	tokenHash := HashToken(token)

	mu.Lock()
	defer mu.Unlock()

	_, err = conn.Exec(`
INSERT INTO server_admin_credentials (username, pass_hash, token_hash)
VALUES (?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    username   = excluded.username,
    pass_hash  = excluded.pass_hash,
    token_hash = excluded.token_hash,
    updated_at = CURRENT_TIMESTAMP
`, username, passHash, tokenHash)
	return err
}

// UpdateAdminPassword replaces the stored password hash
func UpdateAdminPassword(password string) error {
	passHash, err := HashPassword(password)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	_, err = conn.Exec(
		"UPDATE server_admin_credentials SET pass_hash = ?, updated_at = CURRENT_TIMESTAMP",
		passHash,
	)
	return err
}

// UpdateAdminToken replaces the stored token hash
func UpdateAdminToken(token string) error {
	tokenHash := HashToken(token)

	mu.Lock()
	defer mu.Unlock()

	_, err := conn.Exec(
		"UPDATE server_admin_credentials SET token_hash = ?, updated_at = CURRENT_TIMESTAMP",
		tokenHash,
	)
	return err
}

// VerifyAdminPassword returns true if username + password match stored credentials
func VerifyAdminPassword(username, password string) bool {
	mu.RLock()
	defer mu.RUnlock()

	var storedUser, passHash string
	err := conn.QueryRow(
		"SELECT username, pass_hash FROM server_admin_credentials ORDER BY id LIMIT 1",
	).Scan(&storedUser, &passHash)
	if err != nil {
		return false
	}

	// Constant-time username comparison
	if subtle.ConstantTimeCompare([]byte(storedUser), []byte(username)) != 1 {
		return false
	}

	return VerifyPassword(password, passHash)
}

// VerifyAdminToken returns true if the raw token matches the stored hash
func VerifyAdminToken(rawToken string) bool {
	mu.RLock()
	defer mu.RUnlock()

	incoming := HashToken(rawToken)
	var count int
	conn.QueryRow(
		"SELECT COUNT(*) FROM server_admin_credentials WHERE token_hash = ?", incoming,
	).Scan(&count)
	return count > 0
}

// GenerateToken generates a cryptographically secure URL-safe token of the given byte length
func GenerateToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GeneratePassword generates a random human-readable password
func GeneratePassword(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b), nil
}

// HashPassword hashes a password using Argon2id (OWASP 2023 params)
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return "$argon2id$v=19$m=65536,t=3,p=4$" + b64Salt + "$" + b64Hash, nil
}

// VerifyPassword verifies a plaintext password against an Argon2id PHC hash
func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(got, expected) == 1
}

// HashToken returns the SHA-256 hex digest of a raw token
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
