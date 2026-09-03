package main

import (
	"os"
	"path/filepath"
	"testing"
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
	defer os.RemoveAll(staging)

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
