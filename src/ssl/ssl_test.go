package ssl

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestGetTLSConfigOverlayHasNoCertificate verifies overlay domains stay HTTP-only.
func TestGetTLSConfigOverlayHasNoCertificate(t *testing.T) {
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
	if cfg != nil {
		t.Fatalf("expected no TLS config for overlay domain, got %+v", cfg)
	}
	certFile := filepath.Join(certPath, "local", fqdn, "cert.pem")
	if _, err := os.Stat(certFile); !os.IsNotExist(err) {
		t.Errorf("overlay domain unexpectedly created a certificate at %s", certFile)
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

// TestDNS01FailureDoesNotFallBack verifies configured DNS issuance failures
// cannot silently serve a self-signed certificate.
func TestDNS01FailureDoesNotFallBack(t *testing.T) {
	certPath := t.TempDir()
	fqdn := "dns01-test.example"

	m := NewManager(Config{
		Enabled:  true,
		CertPath: certPath,
		LetsEncrypt: LetsEncryptConfig{
			Enabled:     true,
			Email:       "admin@example.com",
			Challenge:   "dns-01",
			DNSProvider: "definitely-not-a-lego-provider",
		},
	})

	if cfg, err := m.GetTLSConfig([]string{fqdn}); err == nil || cfg != nil {
		t.Fatalf("expected DNS issuance failure without fallback, got config=%+v err=%v", cfg, err)
	}
}

func TestDNSProviderRestoresEnvironmentAfterConstruction(t *testing.T) {
	const key = "GITIGNORE_DNS_PROVIDER_TEST_VALUE"
	t.Setenv(key, "original")
	if _, err := dnsProvider("not-a-provider", map[string]string{key: "secret-value"}); err == nil {
		t.Fatal("expected unknown provider error")
	}
	if got := os.Getenv(key); got != "original" {
		t.Fatalf("provider environment was not restored: %q", got)
	}
}

func TestValidateCertificateRequiresCurrentValidityAndHostname(t *testing.T) {
	now := time.Now()
	certificate := &x509.Certificate{
		DNSNames:  []string{"api.example.test"},
		NotBefore: now.Add(-time.Hour),
		NotAfter:  now.Add(time.Hour),
	}
	if err := validateCertificate(certificate, []string{"api.example.test"}, now); err != nil {
		t.Fatalf("valid certificate rejected: %v", err)
	}
	if err := validateCertificate(certificate, []string{"other.example.test"}, now); err == nil {
		t.Fatal("certificate with mismatched hostname was accepted")
	}
	legacyCertificate := &x509.Certificate{Subject: pkix.Name{CommonName: "legacy.example.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	if err := validateCertificate(legacyCertificate, []string{"legacy.example.test"}, now); err != nil {
		t.Fatalf("matching legacy common name rejected: %v", err)
	}
	if err := validateCertificate(legacyCertificate, []string{"other.example.test"}, now); err == nil {
		t.Fatal("mismatched legacy common name was accepted")
	}
	if err := validateCertificate(certificate, []string{"api.example.test"}, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired certificate was accepted")
	}
	if err := validateCertificate(certificate, []string{"api.example.test"}, now.Add(-2*time.Hour)); err == nil {
		t.Fatal("not-yet-valid certificate was accepted")
	}
}

func TestRFC2136CredentialAliasesAndKeyPath(t *testing.T) {
	sslDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sslDir, "acme.key"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareDNSCredentials("rfc2136", map[string]string{
		"RFC2136_NAMESERVER": "ns.example.test",
		"RFC2136_TSIG_FILE":  "acme.key",
	}, sslDir)
	if err != nil {
		t.Fatal(err)
	}
	if prepared["DNSUPDATE_NAMESERVER"] != "ns.example.test" || prepared["DNSUPDATE_TSIG_FILE"] != filepath.Join(sslDir, "acme.key") {
		t.Fatalf("prepared RFC2136 credentials = %#v", prepared)
	}
	if _, err := prepareDNSCredentials("rfc2136", map[string]string{"DNSUPDATE_TSIG_FILE": "../outside.key"}, sslDir); err == nil {
		t.Fatal("path traversal key file was accepted")
	}
}

func TestAppManagedCertificateWiresChallengeHandlers(t *testing.T) {
	for _, challenge := range []string{"http-01", "tls-alpn-01"} {
		t.Run(challenge, func(t *testing.T) {
			root := t.TempDir()
			domain := "challenge-test.example"
			dir := filepath.Join(root, "letsencrypt", domain)
			if err := generateSelfSigned(dir, []string{domain}); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "fullchain.pem")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, "key.pem"), filepath.Join(dir, "privkey.pem")); err != nil {
				t.Fatal(err)
			}
			manager := NewManager(Config{Enabled: true, CertPath: root, LetsEncrypt: LetsEncryptConfig{Enabled: true, Email: "admin@example.com", Challenge: challenge}})
			tlsConfig, err := manager.GetTLSConfig([]string{domain})
			if err != nil {
				t.Fatalf("GetTLSConfig: %v", err)
			}
			if tlsConfig.GetCertificate == nil {
				t.Fatal("app-managed certificate has no dynamic ACME certificate callback")
			}
			if challenge == "tls-alpn-01" {
				found := false
				for _, protocol := range tlsConfig.NextProtos {
					if protocol == "acme-tls/1" {
						found = true
					}
				}
				if !found {
					t.Fatalf("TLS-ALPN protocol missing from NextProtos: %v", tlsConfig.NextProtos)
				}
			}
			certificate, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{ServerName: domain})
			if err != nil || certificate == nil {
				t.Fatalf("GetCertificate = %v, %v", certificate, err)
			}
			fallbackCalled := false
			handler := manager.GetHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fallbackCalled = true
				w.WriteHeader(http.StatusNoContent)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
			if !fallbackCalled || response.Code != http.StatusNoContent {
				t.Fatal("HTTP challenge wrapper did not preserve the server handler")
			}
		})
	}
}
