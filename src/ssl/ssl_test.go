package ssl

import (
	"os"
	"path/filepath"
	"testing"
)

// writeStubCert writes placeholder cert/key files (existence-only) into dir.
func writeStubCert(t *testing.T, dir, certName, keyName string) (certPath, keyPath string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	certPath = filepath.Join(dir, certName)
	keyPath = filepath.Join(dir, keyName)
	if err := os.WriteFile(certPath, []byte("stub-cert"), 0644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("stub-key"), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

// TestFindExistingCertsPrefersLetsEncryptOverLocal verifies priority 3
// ({cert_path}/letsencrypt/{fqdn}) wins over priority 4 ({cert_path}/local/{fqdn})
// when both are present (AI.md PART 15 "Certificate Lookup Order").
func TestFindExistingCertsPrefersLetsEncryptOverLocal(t *testing.T) {
	certPath := t.TempDir()
	fqdn := "cert-order-test.example"

	leCert, _ := writeStubCert(t, filepath.Join(certPath, "letsencrypt", fqdn), "fullchain.pem", "privkey.pem")
	writeStubCert(t, filepath.Join(certPath, "local", fqdn), "cert.pem", "key.pem")

	m := NewManager(Config{Enabled: true, CertPath: certPath})
	gotCert, gotKey := m.findExistingCerts([]string{fqdn})

	if gotCert != leCert {
		t.Errorf("expected Let's Encrypt cert %q to win, got %q", leCert, gotCert)
	}
	if gotKey == "" {
		t.Errorf("expected a key path alongside the cert")
	}
}

// TestFindExistingCertsLocalFallback verifies priority 4 is used when no higher
// priority certificate exists.
func TestFindExistingCertsLocalFallback(t *testing.T) {
	certPath := t.TempDir()
	fqdn := "local-only-test.example"
	localCert, _ := writeStubCert(t, filepath.Join(certPath, "local", fqdn), "cert.pem", "key.pem")

	m := NewManager(Config{Enabled: true, CertPath: certPath})
	gotCert, _ := m.findExistingCerts([]string{fqdn})
	if gotCert != localCert {
		t.Errorf("expected local cert %q, got %q", localCert, gotCert)
	}
}

// TestFindExistingCertsNoneReturnsEmpty verifies no false positives when nothing
// is installed, so the caller proceeds to ACME or self-signed.
func TestFindExistingCertsNoneReturnsEmpty(t *testing.T) {
	m := NewManager(Config{Enabled: true, CertPath: t.TempDir()})
	if c, k := m.findExistingCerts([]string{"absent-test.example"}); c != "" || k != "" {
		t.Errorf("expected no cert, got %q/%q", c, k)
	}
}

// TestGetTLSConfigLocalCheckedBeforeLetsEncrypt verifies an existing local cert
// is loaded even when Let's Encrypt is enabled — local paths are checked first,
// so no network ACME request is attempted (AI.md PART 15).
func TestGetTLSConfigLocalCheckedBeforeLetsEncrypt(t *testing.T) {
	certPath := t.TempDir()
	fqdn := "prefer-local-test.example"

	if err := generateSelfSigned(filepath.Join(certPath, "local", fqdn), []string{fqdn}); err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}

	m := NewManager(Config{
		Enabled:  true,
		CertPath: certPath,
		LetsEncrypt: LetsEncryptConfig{
			Enabled:   true,
			Email:     "admin@example.com",
			Challenge: "http-01",
		},
	})

	cfg, err := m.GetTLSConfig([]string{fqdn})
	if err != nil {
		t.Fatalf("GetTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected the existing local certificate to be loaded, got %+v", cfg)
	}
}

// TestGetTLSConfigOverlayForcesSelfSigned verifies overlay-network domains skip
// Let's Encrypt entirely and generate a self-signed cert under local/{fqdn}
// (AI.md PART 15 overlay handling).
func TestGetTLSConfigOverlayForcesSelfSigned(t *testing.T) {
	certPath := t.TempDir()
	fqdn := "exampleonionaddress.onion"

	m := NewManager(Config{
		Enabled:  true,
		CertPath: certPath,
		LetsEncrypt: LetsEncryptConfig{
			Enabled:   true,
			Email:     "admin@example.com",
			Challenge: "http-01",
		},
	})

	cfg, err := m.GetTLSConfig([]string{fqdn})
	if err != nil {
		t.Fatalf("GetTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected a self-signed certificate, got %+v", cfg)
	}
	certFile := filepath.Join(certPath, "local", fqdn, "cert.pem")
	if _, err := os.Stat(certFile); err != nil {
		t.Errorf("expected self-signed cert at %s: %v", certFile, err)
	}
}

// TestGetTLSConfigDisabledReturnsNil verifies TLS is a no-op when SSL is off.
func TestGetTLSConfigDisabledReturnsNil(t *testing.T) {
	m := NewManager(Config{Enabled: false})
	cfg, err := m.GetTLSConfig([]string{"anything.example"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil TLS config when disabled, got %+v", cfg)
	}
}

// TestDNS01FallsBackToSelfSigned verifies selecting the dns-01 challenge without
// the DNS provider integration falls back to a self-signed cert rather than
// failing (AI.md PART 15 DNS-01 provider config).
func TestDNS01FallsBackToSelfSigned(t *testing.T) {
	certPath := t.TempDir()
	fqdn := "dns01-test.example"

	m := NewManager(Config{
		Enabled:  true,
		CertPath: certPath,
		LetsEncrypt: LetsEncryptConfig{
			Enabled:     true,
			Email:       "admin@example.com",
			Challenge:   "dns-01",
			DNSProvider: "cloudflare",
		},
	})

	cfg, err := m.GetTLSConfig([]string{fqdn})
	if err != nil {
		t.Fatalf("GetTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected self-signed fallback certificate, got %+v", cfg)
	}
}
