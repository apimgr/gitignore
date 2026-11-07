package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesSchemaAndClose(t *testing.T) {
	dataDir := t.TempDir()
	if err := Init(dataDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		if err := Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	dbPath := filepath.Join(dataDir, "db", "gitignore.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected db file at %s: %v", dbPath, err)
	}

	if conn == nil {
		t.Fatal("conn is nil after Init")
	}
	if err := conn.Ping(); err != nil {
		t.Errorf("conn.Ping: %v", err)
	}

	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	want := map[string]bool{
		"server_admin_credentials": false,
		"server_admin_sessions":    false,
		"server_config":            false,
		"server_cluster_state":     false,
		"server_scheduler_state":   false,
		"server_nodes":             false,
		"server_join_tokens":       false,
		"user_accounts":            false,
		"user_tokens":              false,
		"user_sessions":            false,
		"user_invites":             false,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected table %q to exist", name)
		}
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	if err := Init(dataDir); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	defer func() { _ = Close() }()

	if err := Init(dataDir); err != nil {
		t.Fatalf("second Init on existing db: %v", err)
	}
}

func TestInitWithDatabaseDirOverride(t *testing.T) {
	dataDir := t.TempDir()
	overrideDir := t.TempDir()
	t.Setenv("DATABASE_DIR", overrideDir)

	if err := Init(dataDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = Close() }()

	dbPath := filepath.Join(overrideDir, "gitignore.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected db file at override path %s: %v", dbPath, err)
	}

	defaultPath := filepath.Join(dataDir, "db", "gitignore.db")
	if _, err := os.Stat(defaultPath); err == nil {
		t.Errorf("did not expect db file at default path %s when DATABASE_DIR is set", defaultPath)
	}
}

func TestCloseWithoutInitIsSafe(t *testing.T) {
	conn = nil
	if err := Close(); err != nil {
		t.Errorf("Close() with nil conn should be a no-op, got: %v", err)
	}
}

func TestInitRejectsUnwritablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root can bypass permission checks")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(parent, 0700) }()

	target := filepath.Join(parent, "nested")
	if err := Init(target); err == nil {
		_ = Close()
		t.Error("expected Init to fail creating db directory under a read-only parent")
	}
}
