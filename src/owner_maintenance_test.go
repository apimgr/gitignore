package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

func TestWriteOwnerExportContainsMetadataOnlyAndIsPrivate(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	if _, err := db.CreateOwnerWithPrimary("owner-test", "secret-hash", "own_primary", "last", "secret-encrypted-token", now); err != nil {
		t.Fatal(err)
	}
	owner, err := db.GetOwner("owner-test")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := db.ListOwnerTokens("owner-test")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "export.json")
	if err := writeOwnerExport(owner, tokens, path, t.TempDir(), "request-test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := mustStat(t, path).Mode().Perm(); mode != 0600 {
		t.Fatalf("export mode = %04o, want 0600", mode)
	}
	for _, secret := range []string{"secret-hash", "secret-encrypted-token", `"last"`} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("export included secret %q", secret)
		}
	}
	var exported ownerExport
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Format != 1 || exported.Owner.ID != "owner-test" || len(exported.Tokens) != 1 || len(exported.Resources) != 0 || exported.Tokens[0].Prefix != "own_primary" {
		t.Fatalf("unexpected export: %+v", exported)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
