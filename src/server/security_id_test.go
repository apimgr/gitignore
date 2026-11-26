package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

func TestSecurityIDAcceptsPreviousSecretDuringRotationGrace(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	issuedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	issued, err := securityID(issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RotateInstallationSecret(bytes.Repeat([]byte{0x6b}, 32), issuedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !validSecurityID(issued, issuedAt.Add(2*time.Minute)) {
		t.Fatal("security ID signed before rotation was rejected during the grace period")
	}
	if validSecurityID(issued, issuedAt.Add(8*24*time.Hour)) {
		t.Fatal("security ID signed by the old secret remained valid after the grace period")
	}
}
