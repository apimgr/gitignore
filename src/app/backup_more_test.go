package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/gitignore/src/db"
	"github.com/apimgr/gitignore/src/pgp"
)

func TestPrepareBackupSourcesExportsAppSecrets(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT OR REPLACE INTO app_secrets(name, value, updated_at) VALUES(?, ?, ?)", "installation_secret", "encoded-secret", "now")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sources, cleanup, err := prepareBackupSources(nil, configDir, dataDir)
	if err != nil {
		t.Fatalf("prepareBackupSources: %v", err)
	}
	defer cleanup()
	var secretSource string
	for _, source := range sources {
		if source.archivePath == "app_secrets.json" {
			secretSource = source.diskPath
		}
	}
	if secretSource == "" {
		t.Fatal("app_secrets.json was not included")
	}
	var secrets []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	data, err := os.ReadFile(secretSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &secrets); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, secret := range secrets {
		if secret.Name == "installation_secret" && secret.Value == "encoded-secret" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("installation secret missing from exported app secrets: %#v", secrets)
	}
}

// TestCollectBackupSources verifies configuration secrets, user-managed files,
// security files, the database, and custom templates are collected.
func TestCollectBackupSources(t *testing.T) {
	t.Run("empty dirs yield nothing", func(t *testing.T) {
		configDir := t.TempDir()
		dataDir := t.TempDir()
		sources, err := collectBackupSources(configDir, dataDir)
		if err != nil {
			t.Fatalf("collectBackupSources: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected no sources, got %v", sources)
		}
	})

	t.Run("collects server.yml, secrets, user files, database, and templates", func(t *testing.T) {
		configDir := t.TempDir()
		dataDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "secrets.yml"), []byte("server: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, sub := range []string{"yaml.d", "assets", "security"} {
			dir := filepath.Join(configDir, sub)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte(sub), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		dbDir := filepath.Join(dataDir, "db")
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dbDir, "server.db"), []byte("binary-db-data"), 0o600); err != nil {
			t.Fatal(err)
		}
		templateDir := filepath.Join(configDir, "template")
		if err := os.MkdirAll(templateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(templateDir, "custom.html"), []byte("<html></html>"), 0o600); err != nil {
			t.Fatal(err)
		}

		sources, err := collectBackupSources(configDir, dataDir)
		if err != nil {
			t.Fatalf("collectBackupSources: %v", err)
		}

		want := map[string]bool{
			"server.yml":           false,
			"secrets.yml":          false,
			"db/server.db":         false,
			"yaml.d/example.txt":   false,
			"assets/example.txt":   false,
			"security/example.txt": false,
			"template/custom.html": false,
		}
		for _, s := range sources {
			if _, ok := want[s.archivePath]; !ok {
				t.Errorf("unexpected archive path: %s", s.archivePath)
				continue
			}
			want[s.archivePath] = true
		}
		for path, found := range want {
			if !found {
				t.Errorf("expected archive path %s to be collected", path)
			}
		}
	})

	t.Run("skips a template directory that is actually a file-less dir entry", func(t *testing.T) {
		configDir := t.TempDir()
		dataDir := t.TempDir()

		if err := os.MkdirAll(filepath.Join(configDir, "theme"), 0o755); err != nil {
			t.Fatal(err)
		}
		sources, err := collectBackupSources(configDir, dataDir)
		if err != nil {
			t.Fatalf("collectBackupSources: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected no sources from an empty theme dir, got %v", sources)
		}
	})
}

func TestHasRestoredPGPKeypair(t *testing.T) {
	if !hasRestoredPGPKeypair([]string{"security/pgp.pub.asc", "security/pgp.priv.asc.enc"}) {
		t.Fatal("expected complete PGP keypair to be detected")
	}
	if hasRestoredPGPKeypair([]string{"security/pgp.pub.asc"}) {
		t.Fatal("public key without private key must not trigger republishing")
	}
}

func TestVerifyStagedPGPRequiresKeypair(t *testing.T) {
	if err := verifyStagedPGP(t.TempDir()); err == nil {
		t.Fatal("expected missing PGP keypair to fail backup verification")
	}
}

func TestVerifyBackupPGPDecryptsArchivedKey(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	secret, err := db.InstallationSecret()
	if err != nil {
		t.Fatalf("InstallationSecret: %v", err)
	}
	pair, err := pgp.Generate("Test Security <security@example.test>", secret)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	securityDir := filepath.Join(configDir, "security")
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(securityDir, pgpPublicFilename), []byte(pair.PublicKey), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(securityDir, pgpPrivateFilename), pair.EncryptedPrivateKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	sources, err := collectBackupSources(configDir, dataDir)
	if err != nil {
		t.Fatalf("collectBackupSources: %v", err)
	}
	archive, err := buildBackupArchive(sources, false)
	if err != nil {
		t.Fatalf("buildBackupArchive: %v", err)
	}
	backupFile := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(backupFile, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBackupPGP(backupFile, ""); err != nil {
		t.Fatalf("verifyBackupPGP: %v", err)
	}
}

// TestInstallFile verifies atomic install: content and permissions land at
// dest, parent directories are created, and an existing dest is overwritten.
func TestInstallFile(t *testing.T) {
	t.Run("creates parent dirs and copies content", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src.txt")
		if err := os.WriteFile(src, []byte("hello world"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, "nested", "deep", "dest.txt")

		if err := installFile(src, dest); err != nil {
			t.Fatalf("installFile: %v", err)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("ReadFile(dest): %v", err)
		}
		if string(got) != "hello world" {
			t.Errorf("dest content = %q, want %q", got, "hello world")
		}

		fi, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("Stat(dest): %v", err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("dest perm = %v, want 0600", fi.Mode().Perm())
		}
	})

	t.Run("overwrites an existing dest", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src.txt")
		if err := os.WriteFile(src, []byte("new content"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, "dest.txt")
		if err := os.WriteFile(dest, []byte("old content"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := installFile(src, dest); err != nil {
			t.Fatalf("installFile: %v", err)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "new content" {
			t.Errorf("dest content = %q, want %q", got, "new content")
		}
	})

	t.Run("missing src fails", func(t *testing.T) {
		dir := t.TempDir()
		err := installFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dest"))
		if err == nil {
			t.Fatal("expected error for missing src")
		}
	})
}

// TestReadBackupArchive covers both the plaintext and .enc paths, and rejects
// an encrypted archive read back with the wrong password.
func TestReadBackupArchive(t *testing.T) {
	t.Run("plaintext file read back verbatim", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "backup.tar.gz")
		want := []byte("gzip-tar-bytes")
		if err := os.WriteFile(p, want, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readBackupArchive(p, "")
		if err != nil {
			t.Fatalf("readBackupArchive: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("encrypted file decrypts with the right password", func(t *testing.T) {
		dir := t.TempDir()
		plaintext := []byte("secret archive bytes")
		enc, err := encryptBackup(plaintext, "hunter2")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "backup.tar.gz.enc")
		if err := os.WriteFile(p, enc, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readBackupArchive(p, "hunter2")
		if err != nil {
			t.Fatalf("readBackupArchive: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Errorf("got %q, want %q", got, plaintext)
		}
	})

	t.Run("encrypted file rejects wrong password", func(t *testing.T) {
		dir := t.TempDir()
		enc, err := encryptBackup([]byte("secret"), "hunter2")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "backup.tar.gz.enc")
		if err := os.WriteFile(p, enc, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readBackupArchive(p, "wrong"); err == nil {
			t.Fatal("expected wrong password to fail")
		}
	})

	t.Run("missing file fails", func(t *testing.T) {
		if _, err := readBackupArchive(filepath.Join(t.TempDir(), "nope"), ""); err == nil {
			t.Fatal("expected missing file to fail")
		}
	})
}

// TestVerifyBackupFile exercises verifyBackupFile end to end for both an
// unencrypted and an encrypted archive, plus its rejection paths.
func TestVerifyBackupFile(t *testing.T) {
	buildArchive := func(t *testing.T) (dir string, archive []byte) {
		t.Helper()
		dir = t.TempDir()
		cfg := filepath.Join(dir, "server.yml")
		if err := os.WriteFile(cfg, []byte("server:\n  port: 8080\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sources := []backupSource{{archivePath: "server.yml", diskPath: cfg}}
		archive, err := buildBackupArchive(sources, false)
		if err != nil {
			t.Fatalf("buildBackupArchive: %v", err)
		}
		return dir, archive
	}

	t.Run("plaintext archive passes verification", func(t *testing.T) {
		dir, archive := buildArchive(t)
		p := filepath.Join(dir, "backup.tar.gz")
		if err := os.WriteFile(p, archive, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyBackupFile(p, ""); err != nil {
			t.Errorf("verifyBackupFile: %v", err)
		}
	})

	t.Run("encrypted archive passes verification with correct password", func(t *testing.T) {
		dir, archive := buildArchive(t)
		enc, err := encryptBackup(archive, "s3cret")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "backup.tar.gz.enc")
		if err := os.WriteFile(p, enc, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyBackupFile(p, "s3cret"); err != nil {
			t.Errorf("verifyBackupFile: %v", err)
		}
	})

	t.Run("empty file fails", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "backup.tar.gz")
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyBackupFile(p, ""); err == nil {
			t.Fatal("expected empty file to fail verification")
		}
	})

	t.Run("missing file fails", func(t *testing.T) {
		if err := verifyBackupFile(filepath.Join(t.TempDir(), "nope.tar.gz"), ""); err == nil {
			t.Fatal("expected missing file to fail verification")
		}
	})

	t.Run("mismatched checksum fails verification", func(t *testing.T) {
		dir := t.TempDir()
		archive := buildTarGz(t, map[string]string{
			"manifest.json": `{"version":"1.0.0","checksums":{"server.yml":"0000000000000000000000000000000000000000000000000000000000000000"}}`,
			"server.yml":    "server:\n  port: 8080\n",
		})
		p := filepath.Join(dir, "backup.tar.gz")
		if err := os.WriteFile(p, archive, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := verifyBackupFile(p, ""); err == nil {
			t.Fatal("expected mismatched checksum to fail verification")
		}
	})
}

// TestLoadAndVerifyManifestMissing ensures a staging dir with no manifest.json
// fails cleanly instead of installing anything.
func TestLoadAndVerifyManifestMissing(t *testing.T) {
	staging := t.TempDir()
	if _, err := loadAndVerifyManifest(staging); err == nil {
		t.Fatal("expected missing manifest.json to fail")
	}
}

// TestLoadAndVerifyManifestRejectsUnsafePath ensures a manifest listing a
// path-traversal checksum key is rejected before any file read is attempted.
func TestLoadAndVerifyManifestRejectsUnsafePath(t *testing.T) {
	staging := t.TempDir()
	manifest := `{"version":"1.0.0","checksums":{"../escape.txt":"deadbeef"}}`
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAndVerifyManifest(staging); err == nil {
		t.Fatal("expected unsafe manifest path to be rejected")
	}
}

// buildTarGz packs the given name/content pairs into a gzip-compressed tar,
// bypassing extractToStaging's own writer so traversal entries can be forged.
func buildTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractToStagingRejectsTraversalEntry covers a well-formed gzip/tar
// stream whose entry name attempts to climb out of the staging directory,
// distinct from backup_test.go's invalid-gzip-stream case.
func TestExtractToStagingRejectsTraversalEntry(t *testing.T) {
	archive := buildTarGz(t, map[string]string{"../../etc/passwd": "pwned"})
	if _, err := extractToStaging(archive); err == nil {
		t.Fatal("expected path traversal entry to be rejected")
	}
}

// TestExtractToStagingExtractsValidEntries verifies a well-formed archive
// with nested directories extracts its files intact.
func TestExtractToStagingExtractsValidEntries(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"manifest.json":      `{"version":"1.0.0"}`,
		"template/page.html": "<p>hi</p>",
	})
	staging, err := extractToStaging(archive)
	if err != nil {
		t.Fatalf("extractToStaging: %v", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	got, err := os.ReadFile(filepath.Join(staging, "template", "page.html"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "<p>hi</p>" {
		t.Errorf("content = %q, want %q", got, "<p>hi</p>")
	}
}

// TestUpdateNotifyStateRoundtrip covers updateNotifyStatePath, lastNotifiedUpdate,
// and recordNotifiedUpdate together: missing state returns "", a recorded
// version round-trips, and a later record overwrites the earlier one.
func TestUpdateNotifyStateRoundtrip(t *testing.T) {
	t.Run("missing state file returns empty string", func(t *testing.T) {
		dataDir := t.TempDir()
		if got := lastNotifiedUpdate(dataDir); got != "" {
			t.Errorf("lastNotifiedUpdate(missing) = %q, want empty", got)
		}
	})

	t.Run("records and reads back a version", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := recordNotifiedUpdate(dataDir, "v1.2.3"); err != nil {
			t.Fatalf("recordNotifiedUpdate: %v", err)
		}
		if got := lastNotifiedUpdate(dataDir); got != "v1.2.3" {
			t.Errorf("lastNotifiedUpdate = %q, want v1.2.3", got)
		}
	})

	t.Run("overwrites a previously recorded version", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := recordNotifiedUpdate(dataDir, "v1.0.0"); err != nil {
			t.Fatal(err)
		}
		if err := recordNotifiedUpdate(dataDir, "v2.0.0"); err != nil {
			t.Fatal(err)
		}
		if got := lastNotifiedUpdate(dataDir); got != "v2.0.0" {
			t.Errorf("lastNotifiedUpdate = %q, want v2.0.0", got)
		}
	})

	t.Run("creates missing data dir", func(t *testing.T) {
		dataDir := filepath.Join(t.TempDir(), "nested", "data")
		if err := recordNotifiedUpdate(dataDir, "v3.0.0"); err != nil {
			t.Fatalf("recordNotifiedUpdate: %v", err)
		}
		if got := lastNotifiedUpdate(dataDir); got != "v3.0.0" {
			t.Errorf("lastNotifiedUpdate = %q, want v3.0.0", got)
		}
	})

	t.Run("state path lives under the data dir", func(t *testing.T) {
		dataDir := "/tmp/example-data"
		want := filepath.Join(dataDir, "update_last_notified")
		if got := updateNotifyStatePath(dataDir); got != want {
			t.Errorf("updateNotifyStatePath = %q, want %q", got, want)
		}
	})
}
