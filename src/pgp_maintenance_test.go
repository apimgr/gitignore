package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
)

func TestPGPKeyserverEndpoint(t *testing.T) {
	cases := []struct {
		value string
		want  string
		valid bool
	}{
		{" keys.openpgp.org ", "https://keys.openpgp.org/vks/v1/upload", true},
		{"https://keys.openpgp.org", "https://keys.openpgp.org/vks/v1/upload", true},
		{"https://keys.openpgp.org/custom", "https://keys.openpgp.org/custom", true},
		{"", "", false},
		{"http://keys.openpgp.org/upload", "", false},
		{"https://user@keys.openpgp.org/upload", "", false},
		{"https://example.test", "", false},
	}
	for _, tc := range cases {
		got, err := pgpKeyserverEndpoint(tc.value)
		if tc.valid {
			if err != nil || got != tc.want {
				t.Errorf("pgpKeyserverEndpoint(%q) = %q, %v; want %q", tc.value, got, err, tc.want)
			}
		} else if err == nil {
			t.Errorf("pgpKeyserverEndpoint(%q) accepted invalid input as %q", tc.value, got)
		}
	}
}

func TestPGPMaintenanceOutputUsesOutputLanguage(t *testing.T) {
	previous := outputLanguage
	setOutputLanguage("es", "")
	t.Cleanup(func() { outputLanguage = previous })
	if got := cliText("cli.pgp.usage_generate"); got != "Uso: --maintenance pgp generate" {
		t.Errorf("PGP usage = %q, want Spanish translation", got)
	}
}

func TestProjectPGPIdentityUsesConfiguredValuesAndFallbacks(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Branding.Title = " Example Service "
	cfg.Server.Contact.Security.Email = " security@example.test "
	if got := projectPGPIdentity(cfg); got != "Example Service Security <security@example.test>" {
		t.Fatalf("projectPGPIdentity() = %q", got)
	}

	cfg.Server.Branding.Title = " "
	cfg.Server.Contact.Security.Email = ""
	cfg.Web.Security.Contact = "reports@example.test"
	if got := projectPGPIdentity(cfg); got != "gitignore Security <reports@example.test>" {
		t.Fatalf("projectPGPIdentity() fallback = %q", got)
	}
	cfg.Web.Security.Contact = ""
	cfg.Server.FQDN = "example.test"
	if got := projectPGPIdentity(cfg); got != "gitignore Security <security@example.test>" {
		t.Fatalf("projectPGPIdentity() FQDN fallback = %q", got)
	}
}

func TestAtomicPGPWriteAndInstallRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "key.asc")
	if err := atomicPGPWrite(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicPGPWrite(path, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "second" {
		t.Fatalf("atomic write data = %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private key permissions = %04o, want 0600", info.Mode().Perm())
	}

	keyDir := filepath.Join(dir, "security")
	_, err = installPGPFiles(keyDir, []byte("public-one"), []byte("private-one"))
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := installPGPFiles(keyDir, []byte("public-two"), []byte("private-two"))
	if err != nil {
		t.Fatal(err)
	}
	rollback()
	for name, want := range map[string]string{pgpPublicFilename: "public-one", pgpPrivateFilename: "private-one"} {
		got, err := os.ReadFile(filepath.Join(keyDir, name))
		if err != nil || string(got) != want {
			t.Errorf("rollback %s = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestGenerateProjectPGPStoresProtectedKeypairAndMetadata(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "server.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if err := db.Init(dataDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keyDir := filepath.Join(configDir, "security")
	logDir := t.TempDir()
	if err := generateProjectPGP(cfg, keyDir, logDir); err != nil {
		t.Fatalf("generateProjectPGP: %v", err)
	}
	privatePath := filepath.Join(keyDir, pgpPrivateFilename)
	publicPath := filepath.Join(keyDir, pgpPublicFilename)
	privateInfo, err := os.Stat(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if privateInfo.Mode().Perm() != 0600 {
		t.Fatalf("private key permissions = %04o, want 0600", privateInfo.Mode().Perm())
	}
	publicInfo, err := os.Stat(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if publicInfo.Mode().Perm() != 0644 {
		t.Fatalf("public key permissions = %04o, want 0644", publicInfo.Mode().Perm())
	}
	metadata, err := db.GetPGPKeyMetadata()
	if err != nil || metadata.Fingerprint == "" || metadata.Revoked {
		t.Fatalf("PGP metadata = %+v, %v", metadata, err)
	}
	audit, err := os.ReadFile(filepath.Join(logDir, "audit.log"))
	if err != nil || !strings.Contains(string(audit), "security.pgp_generated") {
		t.Fatalf("PGP audit log = %q, %v", audit, err)
	}
	if err := generateProjectPGP(cfg, keyDir, logDir); err == nil {
		t.Fatal("second generation replaced an existing keypair")
	}
}
