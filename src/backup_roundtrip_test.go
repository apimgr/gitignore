package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

func TestExtractToStagingEnforcesRestoreLimits(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gz)
	payload := bytes.Repeat([]byte("x"), 4096)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "payload.bin", Mode: 0o600, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: "second.bin", Mode: 0o600, Size: 0}); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	defaults := config.DefaultConfig().Server.Restore
	if staging, err := extractToStagingWithLimits(archive.Bytes(), defaults); err == nil {
		_ = os.RemoveAll(staging)
	} else {
		t.Fatalf("valid archive rejected: %v", err)
	}
	for name, limits := range map[string]config.RestoreConfig{
		"archive":       {MaxArchiveSize: "1", MaxExpandedSize: "4G", MaxFiles: 100, MaxFileSize: "512M", MaxRatio: 100},
		"file count":    {MaxArchiveSize: "1G", MaxExpandedSize: "4G", MaxFiles: 1, MaxFileSize: "512M", MaxRatio: 100},
		"file size":     {MaxArchiveSize: "1G", MaxExpandedSize: "4G", MaxFiles: 100, MaxFileSize: "1", MaxRatio: 100},
		"expanded size": {MaxArchiveSize: "1G", MaxExpandedSize: "1", MaxFiles: 100, MaxFileSize: "512M", MaxRatio: 100},
		"ratio":         {MaxArchiveSize: "1G", MaxExpandedSize: "4G", MaxFiles: 100, MaxFileSize: "512M", MaxRatio: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if staging, err := extractToStagingWithLimits(archive.Bytes(), limits); err == nil {
				_ = os.RemoveAll(staging)
				t.Fatal("archive exceeded configured limit without error")
			}
		})
	}
}

func TestLoadAndVerifyManifestRejectsUnsafeContents(t *testing.T) {
	staging := t.TempDir()
	manifest, err := json.Marshal(backupManifest{Contents: []string{"../../outside"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAndVerifyManifest(staging); err == nil {
		t.Fatal("unsafe manifest contents path was accepted")
	}
}

func TestVerifyIncrementalBackupRequiresValidBase(t *testing.T) {
	dir := t.TempDir()
	baseBytes, err := buildBackupArchiveAt(nil, false, time.Unix(1, 0), "full", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(dir, "gitignore_backup_2026-01-01.tar.gz")
	checksum := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	writeIncremental := func(baseHash string) string {
		data, err := buildBackupArchiveAt(nil, false, time.Unix(2, 0), "incremental", filepath.Base(basePath), baseHash, nil)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "gitignore-daily.tar.gz")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	incremental := writeIncremental(checksum(baseBytes))
	if err := verifyBackupFile(incremental, ""); err == nil || !strings.Contains(err.Error(), "ERR_BASE_MISSING") {
		t.Fatalf("missing base error = %v", err)
	}
	if err := os.WriteFile(basePath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBackupFile(incremental, ""); err == nil || !strings.Contains(err.Error(), "ERR_BASE_CORRUPT") {
		t.Fatalf("corrupt base error = %v", err)
	}
	if err := os.WriteFile(basePath, baseBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBackupFile(incremental, ""); err != nil {
		t.Fatalf("valid base rejected: %v", err)
	}
}

func TestRunRestoreHonorsMaintenanceDropInLimits(t *testing.T) {
	configDir, dataDir, backupDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(configDir, "yaml.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "yaml.d", "maintenance.yml"), []byte("server:\n  restore:\n    max_archive_size: '1'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := buildBackupArchiveAt(nil, false, time.Now().UTC(), "full", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "backup.tar.gz")
	if err := os.WriteFile(backupPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runRestore(backupPath, configDir, dataDir); err == nil || !strings.Contains(err.Error(), "max_archive_size") {
		t.Fatalf("restore with drop-in archive limit error = %v", err)
	}
}

func TestParseRestoreSize(t *testing.T) {
	for input, want := range map[string]int64{"1": 1, "1K": 1 << 10, "2MB": 2 << 20, "3GiB": 3 << 30, "bad": 99, "0": 99} {
		if got := parseRestoreSize(input, 99); got != want {
			t.Errorf("parseRestoreSize(%q) = %d, want %d", input, got, want)
		}
	}
}

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
	t.Setenv("DATABASE_DIR", "")
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

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
	if !strings.Contains(string(gotServerYML), `port: "8080"`) {
		t.Errorf("restored server.yml does not preserve port: %q", gotServerYML)
	}
	gotSecrets, err := os.ReadFile(filepath.Join(restoreConfigDir, "secrets.yml"))
	if err != nil {
		t.Fatalf("ReadFile restored secrets.yml: %v", err)
	}
	if !strings.Contains(string(gotSecrets), "token: test-token") {
		t.Errorf("restored secrets.yml does not preserve token: %q", gotSecrets)
	}
	if info, err := os.Stat(filepath.Join(restoreConfigDir, "secrets.yml")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("restored secrets.yml mode = %v, %v, want 0600", info, err)
	}

	gotDB, err := os.ReadFile(filepath.Join(restoreDataDir, "db", "server.db"))
	if err != nil {
		t.Fatalf("ReadFile restored server.db: %v", err)
	}
	if len(gotDB) < 16 || string(gotDB[:16]) != "SQLite format 3\x00" {
		t.Errorf("restored server.db is not a valid SQLite database")
	}
}

func TestRunBackupUsesConfiguredEncryptionPassword(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server: {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile server.yml: %v", err)
	}
	t.Setenv("DATABASE_DIR", "")
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

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

func TestScheduledIncrementalBackupRestoresChangesAndDeletions(t *testing.T) {
	configDir, dataDir, backupDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server:\n  port: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	deletedPath := filepath.Join(configDir, "assets", "old.txt")
	if err := os.WriteFile(deletedPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_DIR", "")
	if err := db.Init(dataDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	first := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	if err := runScheduledBackup(cfg, configDir, dataDir, backupDir, "daily", first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server:\n  port: 9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "assets", "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := first.Add(24 * time.Hour)
	if err := runScheduledBackup(cfg, configDir, dataDir, backupDir, "daily", second); err != nil {
		t.Fatal(err)
	}
	incremental := filepath.Join(backupDir, projectName+"-daily.tar.gz")
	restoreConfig, restoreData := t.TempDir(), t.TempDir()
	if err := runRestore(incremental, restoreConfig, restoreData); err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile(filepath.Join(restoreConfig, "server.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(server), `port: "9090"`) {
		t.Fatalf("restored server.yml does not preserve port: %q", server)
	}
	if _, err := os.Stat(filepath.Join(restoreConfig, "assets", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file remains after restore: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(restoreConfig, "assets", "new.txt"))
	if err != nil || string(contents) != "new" {
		t.Fatalf("new file = %q, %v", contents, err)
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
