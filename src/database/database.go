package database

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// Database wraps database operations
type Database struct {
	db     *sql.DB
	dbType string
}

// New creates a new database connection
func New(dbType, connStr string) (*Database, error) {
	var db *sql.DB
	var err error

	switch dbType {
	case "sqlite":
		db, err = sql.Open("sqlite", connStr)
	case "mysql":
		db, err = sql.Open("mysql", connStr)
	case "postgres", "postgresql":
		db, err = sql.Open("postgres", connStr)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Database{
		db:     db,
		dbType: dbType,
	}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// InitSchema initializes the database schema
func (d *Database) InitSchema() error {
	// Settings table
	settingsTable := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		type TEXT NOT NULL CHECK (type IN ('string', 'number', 'boolean', 'json')),
		category TEXT NOT NULL,
		description TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Admin credentials table
	adminCredsTable := `
	CREATE TABLE IF NOT EXISTS admin_credentials (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		token_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Adjust for PostgreSQL
	if d.dbType == "postgres" || d.dbType == "postgresql" {
		settingsTable = `
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('string', 'number', 'boolean', 'json')),
			category TEXT NOT NULL,
			description TEXT,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`

		adminCredsTable = `
		CREATE TABLE IF NOT EXISTS admin_credentials (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`
	}

	// Create tables
	if _, err := d.db.Exec(settingsTable); err != nil {
		return fmt.Errorf("failed to create settings table: %w", err)
	}

	if _, err := d.db.Exec(adminCredsTable); err != nil {
		return fmt.Errorf("failed to create admin_credentials table: %w", err)
	}

	// Insert default settings if not exist
	if err := d.insertDefaultSettings(); err != nil {
		return fmt.Errorf("failed to insert default settings: %w", err)
	}

	return nil
}

// insertDefaultSettings inserts default settings
func (d *Database) insertDefaultSettings() error {
	defaults := map[string]struct {
		value       string
		typ         string
		category    string
		description string
	}{
		"server.title":          {"GitIgnore API", "string", "server", "Server title"},
		"server.address":        {"0.0.0.0", "string", "server", "Listen address"},
		"server.https_enabled":  {"false", "boolean", "server", "Enable HTTPS"},
		"server.trust_proxy":    {"true", "boolean", "server", "Trust reverse proxy headers"},
		"proxy.enabled":         {"true", "boolean", "proxy", "Enable reverse proxy support"},
		"proxy.trust_headers":   {"true", "boolean", "proxy", "Trust proxy headers"},
		"db.type":              {"sqlite", "string", "database", "Database type"},
		"log.level":            {"info", "string", "logging", "Log level"},
		"log.format":           {"json", "string", "logging", "Log format"},
		"log.access":           {"true", "boolean", "logging", "Enable access logging"},
	}

	for key, data := range defaults {
		// Check if key exists
		var count int
		err := d.db.QueryRow("SELECT COUNT(*) FROM settings WHERE key = ?", key).Scan(&count)
		if err != nil && d.dbType != "postgres" {
			return err
		}
		if d.dbType == "postgres" || d.dbType == "postgresql" {
			err = d.db.QueryRow("SELECT COUNT(*) FROM settings WHERE key = $1", key).Scan(&count)
			if err != nil {
				return err
			}
		}

		if count == 0 {
			query := "INSERT INTO settings (key, value, type, category, description) VALUES (?, ?, ?, ?, ?)"
			args := []interface{}{key, data.value, data.typ, data.category, data.description}

			if d.dbType == "postgres" || d.dbType == "postgresql" {
				query = "INSERT INTO settings (key, value, type, category, description) VALUES ($1, $2, $3, $4, $5)"
			}

			if _, err := d.db.Exec(query, args...); err != nil {
				return err
			}
		}
	}

	return nil
}

// hashPassword hashes a password using SHA-256
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%x", hash)
}

// AdminCredentialsExist checks if admin credentials exist
func (d *Database) AdminCredentialsExist() (bool, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM admin_credentials").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateAdminCredentials creates admin credentials
func (d *Database) CreateAdminCredentials(username, password, token string) error {
	passwordHash := hashPassword(password)
	tokenHash := hashPassword(token)

	query := "INSERT INTO admin_credentials (id, username, password_hash, token_hash) VALUES (1, ?, ?, ?)"
	args := []interface{}{username, passwordHash, tokenHash}

	if d.dbType == "postgres" || d.dbType == "postgresql" {
		query = "INSERT INTO admin_credentials (id, username, password_hash, token_hash) VALUES (1, $1, $2, $3)"
	}

	_, err := d.db.Exec(query, args...)
	return err
}

// ValidatePassword validates a password against stored hash
func (d *Database) ValidatePassword(username, password string) (bool, error) {
	var storedHash string
	query := "SELECT password_hash FROM admin_credentials WHERE username = ?"
	args := []interface{}{username}

	if d.dbType == "postgres" || d.dbType == "postgresql" {
		query = "SELECT password_hash FROM admin_credentials WHERE username = $1"
	}

	err := d.db.QueryRow(query, args...).Scan(&storedHash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	passwordHash := hashPassword(password)
	return passwordHash == storedHash, nil
}

// ValidateToken validates an API token against stored hash
func (d *Database) ValidateToken(token string) (bool, error) {
	var storedHash string
	err := d.db.QueryRow("SELECT token_hash FROM admin_credentials LIMIT 1").Scan(&storedHash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	tokenHash := hashPassword(token)
	return tokenHash == storedHash, nil
}

// GetSetting retrieves a setting value
func (d *Database) GetSetting(key string) (string, error) {
	var value string
	query := "SELECT value FROM settings WHERE key = ?"
	args := []interface{}{key}

	if d.dbType == "postgres" || d.dbType == "postgresql" {
		query = "SELECT value FROM settings WHERE key = $1"
	}

	err := d.db.QueryRow(query, args...).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("setting not found: %s", key)
	}
	return value, err
}

// SetSetting sets a setting value
func (d *Database) SetSetting(key, value string) error {
	query := "UPDATE settings SET value = ?, updated_at = ? WHERE key = ?"
	args := []interface{}{value, time.Now(), key}

	if d.dbType == "postgres" || d.dbType == "postgresql" {
		query = "UPDATE settings SET value = $1, updated_at = $2 WHERE key = $3"
	}

	result, err := d.db.Exec(query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("setting not found: %s", key)
	}

	return nil
}

// GetAllSettings retrieves all settings
func (d *Database) GetAllSettings() (map[string]interface{}, error) {
	rows, err := d.db.Query("SELECT key, value, type, category, description FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]interface{})
	for rows.Next() {
		var key, value, typ, category, description string
		if err := rows.Scan(&key, &value, &typ, &category, &description); err != nil {
			return nil, err
		}

		settings[key] = map[string]string{
			"value":       value,
			"type":        typ,
			"category":    category,
			"description": description,
		}
	}

	return settings, rows.Err()
}

// GetDB returns the underlying *sql.DB for custom queries
func (d *Database) GetDB() *sql.DB {
	return d.db
}
