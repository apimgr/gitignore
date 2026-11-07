package tor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestGenerateVanityKeyRejectsInvalidPrefix(t *testing.T) {
	for _, bad := range []string{"hello1", "test!", "abc0", "89"} {
		if _, _, err := GenerateVanityKey(context.Background(), bad); err == nil {
			t.Errorf("prefix %q: expected error (not valid base32)", bad)
		}
	}
}

func TestGenerateVanityKeyFindsShortPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping vanity search in short mode")
	}
	blob, id, err := GenerateVanityKey(context.Background(), "a")
	if err != nil {
		t.Fatalf("GenerateVanityKey: %v", err)
	}
	if !strings.HasPrefix(id, "a") {
		t.Errorf("service ID %q does not start with 'a'", id)
	}
	gotID, err := ServiceIDFromBlob(blob)
	if err != nil {
		t.Fatalf("ServiceIDFromBlob: %v", err)
	}
	if gotID != id {
		t.Errorf("blob service ID = %s, want %s", gotID, id)
	}
}

func TestGenerateVanityKeyRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A long prefix would never be found quickly; a cancelled context must
	// short-circuit the search immediately.
	if _, _, err := GenerateVanityKey(ctx, "zzzz"); err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestServiceIDFromBlobInvalid(t *testing.T) {
	if _, err := ServiceIDFromBlob("not-a-real-blob"); err == nil {
		t.Error("expected error for malformed blob")
	}
}

func TestStageAndApplyVanityKey(t *testing.T) {
	dataDir := t.TempDir()
	blob, wantID := newTestKey(t)

	if err := StageVanityKey(dataDir, blob); err != nil {
		t.Fatalf("StageVanityKey: %v", err)
	}
	stagedID, err := StagedVanityServiceID(dataDir)
	if err != nil {
		t.Fatalf("StagedVanityServiceID: %v", err)
	}
	if stagedID != wantID {
		t.Errorf("staged ID = %s, want %s", stagedID, wantID)
	}

	appliedID, err := ApplyStagedVanityKey(dataDir)
	if err != nil {
		t.Fatalf("ApplyStagedVanityKey: %v", err)
	}
	if appliedID != wantID {
		t.Errorf("applied ID = %s, want %s", appliedID, wantID)
	}

	liveID, err := ServiceIDFromBlob(readFile(t, SecretKeyPath(dataDir)))
	if err != nil {
		t.Fatalf("live key: %v", err)
	}
	if liveID != wantID {
		t.Errorf("live secret key ID = %s, want %s", liveID, wantID)
	}
}

func TestValidateConfigNoBinary(t *testing.T) {
	c := config.DefaultTorConfig()
	c.Binary = "/nonexistent/tor/binary"
	if _, err := ValidateConfig(&c); err == nil {
		t.Error("expected error when configured binary is missing")
	}
}

func TestFindBinaryConfigured(t *testing.T) {
	t.Run("configured path exists", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "tor")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		bin, found := FindBinary(fake)
		if !found || bin != fake {
			t.Errorf("FindBinary(%q) = (%q, %v), want (%q, true)", fake, bin, found, fake)
		}
	})

	t.Run("configured path missing", func(t *testing.T) {
		bin, found := FindBinary(filepath.Join(t.TempDir(), "no-such-tor"))
		if found || bin != "" {
			t.Errorf("FindBinary(missing) = (%q, %v), want (\"\", false)", bin, found)
		}
	})
}

func TestValidateConfigWithConfiguredBinary(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "tor")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	c := config.DefaultTorConfig()
	c.Binary = fake

	bin, err := ValidateConfig(&c)
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	if bin != fake {
		t.Errorf("ValidateConfig() bin = %q, want %q", bin, fake)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
}

func TestReadHostnameMissingFileReturnsEmpty(t *testing.T) {
	dataDir := t.TempDir()
	if got := ReadHostname(dataDir); got != "" {
		t.Errorf("ReadHostname(missing) = %q, want empty", got)
	}
}

func TestReadHostnameReturnsTrimmedContent(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(HostnamePath(dataDir)), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(HostnamePath(dataDir), []byte("abc123.onion\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := ReadHostname(dataDir); got != "abc123.onion" {
		t.Errorf("ReadHostname() = %q, want %q", got, "abc123.onion")
	}
}

func TestStagedVanityServiceIDNoStagedKeyReturnsEmpty(t *testing.T) {
	dataDir := t.TempDir()
	id, err := StagedVanityServiceID(dataDir)
	if err != nil {
		t.Fatalf("StagedVanityServiceID: %v", err)
	}
	if id != "" {
		t.Errorf("StagedVanityServiceID(no staged key) = %q, want empty", id)
	}
}

func TestRegenerateKeysRemovesExistingFiles(t *testing.T) {
	dataDir := t.TempDir()
	blob, _ := newTestKey(t)
	if err := ensureTorFile(SecretKeyPath(dataDir), []byte(blob)); err != nil {
		t.Fatalf("ensureTorFile: %v", err)
	}
	if err := os.WriteFile(HostnamePath(dataDir), []byte("abc123.onion\n"), 0600); err != nil {
		t.Fatalf("WriteFile hostname: %v", err)
	}

	if err := RegenerateKeys(dataDir); err != nil {
		t.Fatalf("RegenerateKeys: %v", err)
	}
	if _, err := os.Stat(SecretKeyPath(dataDir)); !os.IsNotExist(err) {
		t.Errorf("secret key file still exists after RegenerateKeys")
	}
	if _, err := os.Stat(HostnamePath(dataDir)); !os.IsNotExist(err) {
		t.Errorf("hostname file still exists after RegenerateKeys")
	}
}

func TestRegenerateKeysToleratesMissingFiles(t *testing.T) {
	dataDir := t.TempDir()
	if err := RegenerateKeys(dataDir); err != nil {
		t.Errorf("RegenerateKeys(no existing files) = %v, want nil", err)
	}
}

func TestImportKeyValidBlob(t *testing.T) {
	dataDir := t.TempDir()
	blob, wantID := newTestKey(t)
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "imported_key")
	if err := os.WriteFile(srcPath, []byte(blob+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(HostnamePath(dataDir)), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(HostnamePath(dataDir), []byte("old.onion\n"), 0600); err != nil {
		t.Fatalf("WriteFile hostname: %v", err)
	}

	id, err := ImportKey(dataDir, srcPath)
	if err != nil {
		t.Fatalf("ImportKey: %v", err)
	}
	if id != wantID {
		t.Errorf("ImportKey() = %q, want %q", id, wantID)
	}
	if got := readFile(t, SecretKeyPath(dataDir)); got != blob {
		t.Errorf("secret key file = %q, want %q", got, blob)
	}
	if _, err := os.Stat(HostnamePath(dataDir)); !os.IsNotExist(err) {
		t.Errorf("stale hostname file still exists after ImportKey")
	}
}

func TestImportKeyInvalidBlobReturnsWrappedError(t *testing.T) {
	dataDir := t.TempDir()
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "garbage_key")
	if err := os.WriteFile(srcPath, []byte("not a valid key blob"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ImportKey(dataDir, srcPath)
	if err == nil {
		t.Fatal("ImportKey(garbage) = nil error, want error")
	}
	if !strings.Contains(err.Error(), "invalid key file") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "invalid key file")
	}
}

func TestImportKeyMissingSourceReturnsError(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := ImportKey(dataDir, filepath.Join(t.TempDir(), "no-such-file")); err == nil {
		t.Error("ImportKey(missing source) = nil error, want error")
	}
}
