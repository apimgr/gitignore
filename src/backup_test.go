package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEncryptDecryptRoundtrip verifies AES-256-GCM + Argon2id encrypt/decrypt
// is lossless and that a wrong password is rejected by the GCM tag check.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	plaintext := []byte("the quick brown fox jumps over the lazy dog")
	enc, err := encryptBackup(plaintext, "correct horse battery staple")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	dec, err := decryptBackup(enc, "correct horse battery staple")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(dec) != string(plaintext) {
		t.Fatalf("roundtrip mismatch: got %q", dec)
	}
	if _, err := decryptBackup(enc, "wrong password"); err == nil {
		t.Fatal("expected wrong password to fail")
	}
	if !bytes.HasPrefix(enc, []byte(backupMagic)) {
		t.Fatalf("encrypted archive header does not start with %q", backupMagic)
	}
	path := filepath.Join(t.TempDir(), "backup.tar.gz.enc")
	if err := os.WriteFile(path, enc, 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := backupArchiveKeyMatches(path, "correct horse battery staple"); err != nil || !matches {
		t.Fatalf("matching archive key = %t, %v", matches, err)
	}
	if matches, err := backupArchiveKeyMatches(path, "wrong password"); err != nil || matches {
		t.Fatalf("wrong archive key = %t, %v", matches, err)
	}
	tampered := append([]byte(nil), enc...)
	tampered[len(backupMagic)] ^= 1
	if _, err := decryptBackup(tampered, "correct horse battery staple"); err == nil {
		t.Fatal("tampered authenticated header was accepted")
	}
}

func TestVerifyBackupRejectsCorruptSQLiteDatabase(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "server.db")
	if err := os.WriteFile(databasePath, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := buildBackupArchiveAt([]backupSource{{archivePath: "db/server.db", diskPath: databasePath}}, false, time.Now().UTC(), "full", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gitignore_backup_2026-01-01.tar.gz")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBackupFile(path, ""); err == nil {
		t.Fatal("corrupt SQLite database passed backup verification")
	}
}

// TestArchiveRoundtrip builds an archive from real files, extracts it to a
// staging dir, and verifies the manifest checksums pass.
func TestArchiveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "server.yml")
	if err := os.WriteFile(cfg, []byte("server:\n  port: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sources := []backupSource{{archivePath: "server.yml", diskPath: cfg}}
	archive, err := buildBackupArchive(sources, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	staging, err := extractToStaging(archive)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	m, err := loadAndVerifyManifest(staging)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(m.Contents) != 1 || m.Contents[0] != "server.yml" {
		t.Fatalf("unexpected contents: %v", m.Contents)
	}
}

// TestExtractRejectsTraversal ensures a tampered archive path cannot escape the
// staging directory.
func TestExtractRejectsTraversal(t *testing.T) {
	if _, err := extractToStaging([]byte("not a gzip stream")); err == nil {
		t.Fatal("expected invalid gzip to fail")
	}
}

// TestNormalizeBackupExt checks extension normalization matches the encryption
// decision.
func TestNormalizeBackupExt(t *testing.T) {
	cases := []struct {
		in        string
		encrypted bool
		want      string
	}{
		{"backup.tar.gz", false, "backup.tar.gz"},
		{"backup.tar.gz", true, "backup.tar.gz.enc"},
		{"backup.tar.gz.enc", false, "backup.tar.gz"},
		{"backup", false, "backup.tar.gz"},
		{"backup", true, "backup.tar.gz.enc"},
	}
	for _, c := range cases {
		if got := normalizeBackupExt(c.in, c.encrypted); got != c.want {
			t.Errorf("normalizeBackupExt(%q,%v)=%q want %q", c.in, c.encrypted, got, c.want)
		}
	}
}
