package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// Config holds SSL/TLS configuration
type Config struct {
	Enabled     bool
	CertPath    string
	LetsEncrypt LetsEncryptConfig
}

// LetsEncryptConfig holds Let's Encrypt settings. DNSProvider selects a DNS-01
// provider (AI.md PART 15); its credentials arrive already decrypted in
// DNSCredentials, keyed by credential field name.
type LetsEncryptConfig struct {
	Enabled     bool
	Email       string
	Challenge   string // http-01, tls-alpn-01, dns-01
	DNSProvider string
	// DNSCredentials carries the decrypted provider credential fields. It is
	// populated in memory only — never persisted in plaintext.
	DNSCredentials map[string]string
}

// Manager handles SSL/TLS certificates
type Manager struct {
	config      Config
	certManager *autocert.Manager
	mu          sync.RWMutex
}

// NewManager creates a new SSL manager
func NewManager(cfg Config) *Manager {
	return &Manager{
		config: cfg,
	}
}

// GetTLSConfig returns TLS configuration for the server. Certificate discovery
// follows the mandated order (AI.md PART 15 "Certificate Lookup Order"): ALL
// local certificate locations are checked BEFORE requesting a new Let's Encrypt
// certificate. Overlay-network domains (.onion/.i2p/.exit) always use a
// self-signed certificate because public CAs cannot validate them.
func (m *Manager) GetTLSConfig(domains []string) (*tls.Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		return nil, nil
	}

	// Priority 1-4: existing local certificates are always preferred over a new
	// ACME request.
	if cert, key := m.findExistingCerts(domains); cert != "" && key != "" {
		log.Printf("Using existing certificate: %s", cert)
		return loadCertConfig(cert, key)
	}

	// Overlay networks cannot use Let's Encrypt — force a self-signed cert.
	if isOverlayDomain(domains) {
		log.Printf("Overlay-network domain detected; using self-signed certificate")
		return m.selfSignedTLSConfig(domains)
	}

	// Request a new certificate via Let's Encrypt when enabled.
	if m.config.LetsEncrypt.Enabled {
		cfg, err := m.getLetsEncryptTLSConfig(domains)
		if err == nil {
			return cfg, nil
		}
		log.Printf("Let's Encrypt unavailable (%v); falling back to self-signed certificate", err)
	}

	// Final fallback so TLS always works: generate a self-signed certificate.
	return m.selfSignedTLSConfig(domains)
}

// getLetsEncryptTLSConfig configures autocert for Let's Encrypt. The autocert
// cache is stored under {cert_path}/letsencrypt so app-managed certificates
// live beside manually installed ones (AI.md PART 15). The dns-01 challenge
// requires an external DNS provider integration that autocert does not provide;
// when selected without that integration the caller falls back to self-signed.
func (m *Manager) getLetsEncryptTLSConfig(domains []string) (*tls.Config, error) {
	if m.config.LetsEncrypt.Challenge == "dns-01" {
		return nil, fmt.Errorf("dns-01 challenge for provider %q requires the DNS provider integration", m.config.LetsEncrypt.DNSProvider)
	}

	cacheDir := filepath.Join(m.config.CertPath, "letsencrypt")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert cache dir: %w", err)
	}

	m.certManager = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
		Cache:      autocert.DirCache(cacheDir),
		Email:      m.config.LetsEncrypt.Email,
	}

	return m.certManager.TLSConfig(), nil
}

// GetHTTPHandler returns HTTP handler for ACME challenges
func (m *Manager) GetHTTPHandler(fallback http.Handler) http.Handler {
	if m.certManager != nil {
		return m.certManager.HTTPHandler(fallback)
	}
	return fallback
}

// findExistingCerts looks for existing certificates in the mandated priority
// order (AI.md PART 15 "Certificate Lookup Order"):
//
//	1. /etc/letsencrypt/live/domain/     (literal shared directory)
//	2. /etc/letsencrypt/live/{fqdn}/     (certbot per-FQDN directory)
//	3. {cert_path}/letsencrypt/{fqdn}/   (app-managed Let's Encrypt certs)
//	4. {cert_path}/local/{fqdn}/         (self-signed / user-provided certs)
func (m *Manager) findExistingCerts(domains []string) (certPath, keyPath string) {
	// 1. Literal /etc/letsencrypt/live/domain/ shared setup.
	if c, k := certPair("/etc/letsencrypt/live/domain", "fullchain.pem", "privkey.pem"); c != "" {
		return c, k
	}

	for _, domain := range domains {
		if domain == "" {
			continue
		}
		// 2. /etc/letsencrypt/live/{fqdn}/
		if c, k := certPair(filepath.Join("/etc/letsencrypt/live", domain), "fullchain.pem", "privkey.pem"); c != "" {
			return c, k
		}
		if m.config.CertPath != "" {
			// 3. {cert_path}/letsencrypt/{fqdn}/
			if c, k := certPair(filepath.Join(m.config.CertPath, "letsencrypt", domain), "fullchain.pem", "privkey.pem"); c != "" {
				return c, k
			}
			// 4. {cert_path}/local/{fqdn}/
			if c, k := certPair(filepath.Join(m.config.CertPath, "local", domain), "cert.pem", "key.pem"); c != "" {
				return c, k
			}
		}
	}
	return "", ""
}

// selfSignedTLSConfig loads (or generates) a self-signed certificate for the
// domain under {cert_path}/local/{fqdn}/ (AI.md PART 15 "Certificate Directory
// Structure"). User-managed: the app never auto-renews certs in this directory.
func (m *Manager) selfSignedTLSConfig(domains []string) (*tls.Config, error) {
	fqdn := "localhost"
	if len(domains) > 0 && domains[0] != "" {
		fqdn = domains[0]
	}

	dir := filepath.Join(m.config.CertPath, "local", fqdn)
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if fileExists(certPath) && fileExists(keyPath) {
		log.Printf("Using self-signed certificate: %s", certPath)
		return loadCertConfig(certPath, keyPath)
	}

	if err := generateSelfSigned(dir, domains); err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}
	log.Printf("Generated self-signed certificate: %s", certPath)
	return loadCertConfig(certPath, keyPath)
}

// generateSelfSigned writes a new self-signed cert.pem/key.pem pair into dir,
// valid for one year and covering every configured domain (SAN).
func generateSelfSigned(dir string, domains []string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	cn := "localhost"
	if len(domains) > 0 && domains[0] != "" {
		cn = domains[0]
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, d := range domains {
		if d == "" {
			continue
		}
		if ip := net.ParseIP(d); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, d)
		}
	}
	if len(tmpl.DNSNames) == 0 && len(tmpl.IPAddresses) == 0 {
		tmpl.DNSNames = append(tmpl.DNSNames, "localhost")
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	if err := writePEM(filepath.Join(dir, "cert.pem"), "CERTIFICATE", der, 0644); err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	return writePEM(filepath.Join(dir, "key.pem"), "EC PRIVATE KEY", keyDER, 0600)
}

// writePEM encodes der bytes into a PEM block of the given type at path.
func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

// loadCertConfig loads a cert/key pair into a minimal TLS 1.2+ config.
func loadCertConfig(certPath, keyPath string) (*tls.Config, error) {
	tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// certPair returns the cert and key paths under dir when both exist.
func certPair(dir, certName, keyName string) (certPath, keyPath string) {
	cert := filepath.Join(dir, certName)
	key := filepath.Join(dir, keyName)
	if fileExists(cert) && fileExists(key) {
		return cert, key
	}
	return "", ""
}

// isOverlayDomain reports whether any domain is an overlay-network address
// (.onion/.i2p/.exit) that cannot obtain a public CA certificate.
func isOverlayDomain(domains []string) bool {
	for _, d := range domains {
		lower := strings.ToLower(d)
		if strings.HasSuffix(lower, ".onion") ||
			strings.HasSuffix(lower, ".i2p") ||
			strings.HasSuffix(lower, ".exit") {
			return true
		}
	}
	return false
}

// ChallengeServer handles ACME HTTP-01 challenges
type ChallengeServer struct {
	tokens map[string]string
	mu     sync.RWMutex
}

// NewChallengeServer creates a challenge server
func NewChallengeServer() *ChallengeServer {
	return &ChallengeServer{
		tokens: make(map[string]string),
	}
}

// SetToken sets a challenge token
func (cs *ChallengeServer) SetToken(token, auth string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.tokens[token] = auth
}

// ClearToken removes a challenge token
func (cs *ChallengeServer) ClearToken(token string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.tokens, token)
}

// ServeHTTP handles ACME challenge requests
func (cs *ChallengeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		return false
	}

	token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	cs.mu.RLock()
	auth, ok := cs.tokens[token]
	cs.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return true
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(auth))
	return true
}

// ParseChallenge parses challenge type from string
func ParseChallenge(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "http-01", "http01", "http":
		return "http-01"
	case "tls-alpn-01", "tlsalpn01", "tls-alpn", "tls":
		return "tls-alpn-01"
	case "dns-01", "dns01", "dns":
		return "dns-01"
	default:
		return "http-01"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
