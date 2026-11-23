package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

// runBackup followed by runRestore exercises the full unencrypted
// backup/restore cycle end to end, over tmp-only paths (configDir, dataDir,
// backupFile are all parameters — no shared/global state is touched).
func TestRunBackupRunRestoreRoundtrip(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server:\n  port: \"8080\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile server.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "secrets.yml"), []byte("server:\n  token: test-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile secrets.yml: %v", err)
	}
	dbDir := filepath.Join(dataDir, "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("MkdirAll db dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "server.db"), []byte("fake-sqlite-contents"), 0o644); err != nil {
		t.Fatalf("WriteFile server.db: %v", err)
	}

	backupFile := filepath.Join(t.TempDir(), "backup.tar.gz")
	cfg := &config.Config{}
	cfg.Server.Backup.Encryption.Enabled = false

	if err := runBackup(cfg, configDir, dataDir, backupFile); err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	if _, err := os.Stat(backupFile); err != nil {
		t.Fatalf("backup file not created: %v", err)
	}

	// Restore into fresh destination directories to verify the archive
	// actually round-trips the original file contents.
	restoreConfigDir := t.TempDir()
	restoreDataDir := t.TempDir()
	if err := runRestore(backupFile, restoreConfigDir, restoreDataDir); err != nil {
		t.Fatalf("runRestore: %v", err)
	}

	gotServerYML, err := os.ReadFile(filepath.Join(restoreConfigDir, "server.yml"))
	if err != nil {
		t.Fatalf("ReadFile restored server.yml: %v", err)
	}
	if string(gotServerYML) != "server:\n  port: \"8080\"\n" {
		t.Errorf("restored server.yml = %q, want original contents", gotServerYML)
	}
	gotSecrets, err := os.ReadFile(filepath.Join(restoreConfigDir, "secrets.yml"))
	if err != nil {
		t.Fatalf("ReadFile restored secrets.yml: %v", err)
	}
	if string(gotSecrets) != "server:\n  token: test-token\n" {
		t.Errorf("restored secrets.yml = %q, want original contents", gotSecrets)
	}
	if info, err := os.Stat(filepath.Join(restoreConfigDir, "secrets.yml")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("restored secrets.yml mode = %v, %v, want 0600", info, err)
	}

	gotDB, err := os.ReadFile(filepath.Join(restoreDataDir, "db", "server.db"))
	if err != nil {
		t.Fatalf("ReadFile restored server.db: %v", err)
	}
	if string(gotDB) != "fake-sqlite-contents" {
		t.Errorf("restored server.db = %q, want original contents", gotDB)
	}
}

func TestRunBackupUsesConfiguredEncryptionPassword(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server: {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile server.yml: %v", err)
	}
	dbDir := filepath.Join(dataDir, "db")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		t.Fatalf("MkdirAll db dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "server.db"), []byte("backup contents"), 0o600); err != nil {
		t.Fatalf("WriteFile server.db: %v", err)
	}

	password := "configured backup password"
	cfg := config.DefaultConfig()
	cfg.Server.Backup.EncryptionPassword = password
	backupPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := runBackup(cfg, configDir, dataDir, backupPath); err != nil {
		t.Fatalf("runBackup with configured password: %v", err)
	}
	encryptedBackupPath := normalizeBackupExt(backupPath, true)
	if filepath.Ext(encryptedBackupPath) != ".enc" {
		t.Fatalf("normalized backup path = %q, want encrypted .enc extension", encryptedBackupPath)
	}
	if _, err := os.Stat(encryptedBackupPath); err != nil {
		t.Fatalf("encrypted backup was not written at %q: %v", encryptedBackupPath, err)
	}
	archive, err := readBackupArchive(encryptedBackupPath, password)
	if err != nil {
		t.Fatalf("read encrypted backup: %v", err)
	}
	staging, err := extractToStaging(archive)
	if err != nil {
		t.Fatalf("extract encrypted backup: %v", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if _, err := loadAndVerifyManifest(staging); err != nil {
		t.Fatalf("verify encrypted backup manifest: %v", err)
	}
}

func TestRunBackupBlocksComplianceWithoutConfiguredPassword(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Compliance.Enabled = true
	backupPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	err := runBackup(cfg, t.TempDir(), t.TempDir(), backupPath)
	if err == nil {
		t.Fatal("runBackup in compliance mode without password succeeded")
	}
	if _, statErr := os.Stat(backupPath); !os.IsNotExist(statErr) {
		t.Fatalf("backup file exists after rejected backup: %v", statErr)
	}
}

// runBackup with no config/database present must fail rather than write an
// empty archive.
func TestRunBackupNothingToBackUp(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	backupFile := filepath.Join(t.TempDir(), "backup.tar.gz")

	if err := runBackup(nil, configDir, dataDir, backupFile); err == nil {
		t.Error("runBackup() with no sources = nil error, want error")
	}
}

// runRestore against a missing backup file must return an error without
// touching the destination directories.
func TestRunRestoreMissingFile(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	if err := runRestore(filepath.Join(t.TempDir(), "no-such-backup.tar.gz"), configDir, dataDir); err == nil {
		t.Error("runRestore(missing file) = nil error, want error")
	}
}
