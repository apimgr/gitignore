package tor

import (
	"context"
	"os"
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
}
