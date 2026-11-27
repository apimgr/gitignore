package ssl

import (
	"context"
	"crypto"
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

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/providers/dns"
	"github.com/go-acme/lego/v5/registration"
	xacme "golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

var dnsEnvironmentMu sync.Mutex

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
	OnDNSValidated func(time.Time) error
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
// follows AI.md PART 15: local certificate paths are checked before ACME, while
// overlay-network domains never receive a certificate.
func (m *Manager) GetTLSConfig(domains []string) (*tls.Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		return nil, nil
	}
	if isOverlayDomain(domains) {
		return nil, nil
	}
	if m.config.LetsEncrypt.Enabled && ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
		credentials, err := prepareDNSCredentials(m.config.LetsEncrypt.DNSProvider, m.config.LetsEncrypt.DNSCredentials, m.config.CertPath)
		if err != nil {
			return nil, err
		}
		if _, err := dnsProvider(m.config.LetsEncrypt.DNSProvider, credentials); err != nil {
			return nil, fmt.Errorf("DNS provider %q validation failed", m.config.LetsEncrypt.DNSProvider)
		}
	}

	// Priority 1-4: existing local certificates are always preferred over a new
	// ACME request.
	renewalCertPath, renewalKeyPath := "", ""
	if cert, key := m.findExistingCerts(domains); cert != "" && key != "" {
		dnsChallenge := m.config.LetsEncrypt.Enabled && ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01"
		appManaged := filepath.Clean(cert) == filepath.Clean(filepath.Join(m.config.CertPath, "letsencrypt", firstDomain(domains), "fullchain.pem"))
		renewalDue := appManaged && m.config.LetsEncrypt.Enabled && certificateRenewalDue(cert)
		if renewalDue {
			renewalCertPath, renewalKeyPath = cert, key
		} else {
			log.Printf("Using existing certificate: %s", cert)
			loaded, err := loadCertConfig(cert, key, domains)
			if err != nil {
				return nil, err
			}
			if appManaged && m.config.LetsEncrypt.Enabled {
				if dnsChallenge {
					m.attachDNSCertificateReloader(loaded, cert, key, domains)
				} else if acmeConfig, err := m.getLetsEncryptTLSConfig(domains); err == nil {
					loaded.NextProtos = append([]string(nil), acmeConfig.NextProtos...)
					loaded.GetCertificate = m.autocertCertificateProvider(acmeConfig.GetCertificate, cert, key, domains)
				} else {
					return nil, err
				}
			}
			return loaded, nil
		}
	}

	// Request a new certificate via Let's Encrypt when enabled.
	if m.config.LetsEncrypt.Enabled {
		cfg, err := m.getLetsEncryptTLSConfig(domains)
		if err == nil {
			if renewalCertPath != "" && cfg.GetCertificate != nil {
				cfg.GetCertificate = m.autocertCertificateProvider(cfg.GetCertificate, renewalCertPath, renewalKeyPath, domains)
			}
			return cfg, nil
		}
		if ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
			return nil, fmt.Errorf("let's Encrypt DNS-01 certificate request failed for provider %q", m.config.LetsEncrypt.DNSProvider)
		}
		log.Printf("Let's Encrypt unavailable (%v); falling back to self-signed certificate", err)
	}

	// Final fallback so TLS always works: generate a self-signed certificate.
	return m.selfSignedTLSConfig(domains)
}

func (m *Manager) autocertCertificateProvider(acquire func(*tls.ClientHelloInfo) (*tls.Certificate, error), certPath, keyPath string, domains []string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if !certificateRenewalDue(certPath) {
			if loaded, err := loadCertConfig(certPath, keyPath, domains); err == nil {
				return &loaded.Certificates[0], nil
			}
		}
		certificate, err := acquire(hello)
		if err != nil {
			return nil, err
		}
		leaf := certificate.Leaf
		if leaf == nil && len(certificate.Certificate) > 0 {
			leaf, err = x509.ParseCertificate(certificate.Certificate[0])
			if err != nil {
				return nil, fmt.Errorf("failed to parse renewed certificate")
			}
		}
		if leaf == nil {
			return nil, fmt.Errorf("renewed certificate contains no leaf certificate")
		}
		if err := validateCertificate(leaf, domains, time.Now()); err != nil {
			return nil, err
		}
		if err := persistTLSCertificate(filepath.Dir(certPath), certificate); err != nil {
			return nil, err
		}
		return certificate, nil
	}
}

func certificateRenewalDue(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return true
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	return time.Until(certificate.NotAfter) <= 30*24*time.Hour
}

// getLetsEncryptTLSConfig configures the selected ACME challenge.
func (m *Manager) getLetsEncryptTLSConfig(domains []string) (*tls.Config, error) {
	challengeType := ParseChallenge(m.config.LetsEncrypt.Challenge)
	if challengeType == "dns-01" {
		return m.obtainDNSCertificate(context.Background(), domains)
	}

	cacheDir := filepath.Join(m.config.CertPath, "letsencrypt", ".autocert-cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert cache dir: %w", err)
	}

	m.certManager = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
		Cache:      autocert.DirCache(cacheDir),
		Email:      m.config.LetsEncrypt.Email,
	}

	config := m.certManager.TLSConfig()
	if challengeType == "http-01" {
		protocols := config.NextProtos[:0]
		for _, protocol := range config.NextProtos {
			if protocol != xacme.ALPNProto {
				protocols = append(protocols, protocol)
			}
		}
		config.NextProtos = protocols
	}
	return config, nil
}

type acmeUser struct {
	email   string
	key     crypto.Signer
	account *acme.ExtendedAccount
}

func (u *acmeUser) GetEmail() string                       { return u.email }
func (u *acmeUser) GetRegistration() *acme.ExtendedAccount { return u.account }
func (u *acmeUser) GetPrivateKey() crypto.Signer           { return u.key }

func (m *Manager) obtainDNSCertificate(ctx context.Context, domains []string) (*tls.Config, error) {
	providerName := strings.ToLower(strings.TrimSpace(m.config.LetsEncrypt.DNSProvider))
	credentials, err := prepareDNSCredentials(providerName, m.config.LetsEncrypt.DNSCredentials, m.config.CertPath)
	if err != nil {
		return nil, err
	}
	provider, err := dnsProvider(providerName, credentials)
	if err != nil {
		return nil, fmt.Errorf("DNS provider %q validation failed", providerName)
	}
	root := filepath.Join(m.config.CertPath, "letsencrypt")
	cacheDir := filepath.Join(root, firstDomain(domains))
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create ACME certificate directory")
	}
	accountKey, err := loadOrCreateAccountKey(filepath.Join(root, "account.key"))
	if err != nil {
		return nil, fmt.Errorf("cannot load ACME account key")
	}
	user := &acmeUser{email: m.config.LetsEncrypt.Email, key: accountKey}
	legoClient, err := lego.NewClient(lego.NewConfig(user))
	if err != nil {
		return nil, fmt.Errorf("cannot initialize ACME client")
	}
	if err := legoClient.Challenge.SetDNS01Provider(provider); err != nil {
		return nil, fmt.Errorf("cannot configure DNS challenge provider")
	}
	account, err := legoClient.Registration.ResolveAccountByKey(ctx)
	if err != nil {
		account, err = legoClient.Registration.Register(ctx, registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("ACME account registration failed")
		}
	}
	user.account = account
	resource, err := legoClient.Certificate.Obtain(ctx, certificate.ObtainRequest{Domains: domains, Bundle: true})
	if err != nil {
		return nil, fmt.Errorf("ACME certificate issuance failed")
	}
	if m.config.LetsEncrypt.OnDNSValidated != nil {
		if err := m.config.LetsEncrypt.OnDNSValidated(time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("cannot record DNS credential validation time")
		}
	}
	certPath := filepath.Join(cacheDir, "fullchain.pem")
	keyPath := filepath.Join(cacheDir, "privkey.pem")
	if err := writePrivateFile(certPath, resource.Certificate, 0600); err != nil {
		return nil, fmt.Errorf("cannot persist ACME certificate")
	}
	if err := writePrivateFile(keyPath, resource.PrivateKey, 0600); err != nil {
		return nil, fmt.Errorf("cannot persist ACME private key")
	}
	loaded, err := loadCertConfig(certPath, keyPath, domains)
	if err != nil {
		return nil, err
	}
	m.attachDNSCertificateReloader(loaded, certPath, keyPath, domains)
	return loaded, nil
}

func (m *Manager) attachDNSCertificateReloader(cfg *tls.Config, certPath, keyPath string, domains []string) {
	cfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		loaded, err := loadCertConfig(certPath, keyPath, domains)
		if err != nil {
			return nil, err
		}
		return &loaded.Certificates[0], nil
	}
}

// Renew checks the app-managed certificate for the fixed 30-day renewal
// window and requests a replacement with the configured ACME challenge.
func (m *Manager) Renew(ctx context.Context, domains []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.config.Enabled || !m.config.LetsEncrypt.Enabled {
		return nil
	}
	if len(domains) == 0 || isOverlayDomain(domains) {
		return nil
	}
	appCert := filepath.Join(m.config.CertPath, "letsencrypt", domains[0], "fullchain.pem")
	if existing, _ := m.findExistingCerts(domains); existing != "" {
		if filepath.Clean(existing) != filepath.Clean(appCert) {
			return nil
		}
		if !certificateRenewalDue(existing) {
			return nil
		}
	}
	if ParseChallenge(m.config.LetsEncrypt.Challenge) == "dns-01" {
		_, err := m.obtainDNSCertificate(ctx, domains)
		return err
	}
	if m.certManager == nil {
		if _, err := m.getLetsEncryptTLSConfig(domains); err != nil {
			return err
		}
	}
	certificate, err := m.certManager.GetCertificate(&tls.ClientHelloInfo{ServerName: firstDomain(domains)})
	if err != nil {
		return err
	}
	return persistTLSCertificate(filepath.Join(m.config.CertPath, "letsencrypt", firstDomain(domains)), certificate)
}

func persistTLSCertificate(dir string, certificate *tls.Certificate) error {
	if certificate == nil || len(certificate.Certificate) == 0 {
		return fmt.Errorf("ACME returned an empty certificate")
	}
	var chain []byte
	for _, der := range certificate.Certificate {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	if err := writePrivateFile(filepath.Join(dir, "fullchain.pem"), chain, 0600); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return writePrivateFile(filepath.Join(dir, "privkey.pem"), keyPEM, 0600)
}

func prepareDNSCredentials(provider string, credentials map[string]string, sslDir string) (map[string]string, error) {
	prepared := make(map[string]string, len(credentials))
	for name, value := range credentials {
		prepared[name] = value
	}
	if provider == "rfc2136" || provider == "dnsupdate" {
		for name, value := range credentials {
			if !strings.HasPrefix(name, "RFC2136_") {
				continue
			}
			legoName := "DNSUPDATE_" + strings.TrimPrefix(name, "RFC2136_")
			if _, exists := prepared[legoName]; !exists {
				prepared[legoName] = value
			}
		}
		for _, name := range []string{"DNSUPDATE_TSIG_FILE"} {
			value := prepared[name]
			if value == "" {
				continue
			}
			if filepath.Base(value) != value || value == "." || value == ".." {
				return nil, fmt.Errorf("invalid RFC 2136 key file name")
			}
			path := filepath.Join(sslDir, value)
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("invalid RFC 2136 key file")
			}
			prepared[name] = path
		}
	}
	return prepared, nil
}

func dnsProvider(name string, credentials map[string]string) (challenge.Provider, error) {
	if name == "" {
		return nil, fmt.Errorf("DNS provider is empty")
	}
	dnsEnvironmentMu.Lock()
	defer dnsEnvironmentMu.Unlock()
	type priorValue struct {
		value   string
		present bool
	}
	prior := make(map[string]priorValue, len(credentials))
	defer func() {
		for key, old := range prior {
			if old.present {
				_ = os.Setenv(key, old.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}()
	for key, value := range credentials {
		old, present := os.LookupEnv(key)
		prior[key] = priorValue{old, present}
		if err := os.Setenv(key, value); err != nil {
			return nil, fmt.Errorf("cannot set DNS provider environment")
		}
	}
	return dns.NewDNSChallengeProviderByName(name)
}

func firstDomain(domains []string) string {
	if len(domains) == 0 || strings.TrimSpace(domains[0]) == "" {
		return "localhost"
	}
	return domains[0]
}

func loadOrCreateAccountKey(path string) (crypto.Signer, error) {
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("invalid account key")
		}
		return x509.ParseECPrivateKey(block.Bytes)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writePrivateFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func writePrivateFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".acme-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// GetHTTPHandler returns HTTP handler for ACME challenges
func (m *Manager) GetHTTPHandler(fallback http.Handler) http.Handler {
	if ParseChallenge(m.config.LetsEncrypt.Challenge) != "http-01" {
		return fallback
	}
	m.mu.RLock()
	manager := m.certManager
	m.mu.RUnlock()
	if manager != nil {
		return manager.HTTPHandler(fallback)
	}
	return fallback
}

// findExistingCerts looks for existing certificates in the mandated priority
// order (AI.md PART 15 "Certificate Lookup Order"):
//
//  1. /etc/letsencrypt/live/domain/     (literal shared directory)
//  2. /etc/letsencrypt/live/{fqdn}/     (certbot per-FQDN directory)
//  3. {cert_path}/letsencrypt/{fqdn}/   (app-managed Let's Encrypt certs)
//  4. {cert_path}/local/{fqdn}/         (self-signed / user-provided certs)
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
		return loadCertConfig(certPath, keyPath, domains)
	}

	if err := generateSelfSigned(dir, domains); err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}
	log.Printf("Generated self-signed certificate: %s", certPath)
	return loadCertConfig(certPath, keyPath, domains)
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
	defer func() { _ = f.Close() }()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

// loadCertConfig loads a cert/key pair into a minimal TLS 1.2+ config.
func loadCertConfig(certPath, keyPath string, domains []string) (*tls.Config, error) {
	tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}
	leaf := tlsCert.Leaf
	if leaf == nil && len(tlsCert.Certificate) > 0 {
		leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %w", err)
		}
	}
	if leaf == nil {
		return nil, fmt.Errorf("certificate contains no leaf certificate")
	}
	if err := validateCertificate(leaf, domains, time.Now()); err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func validateCertificate(certificate *x509.Certificate, domains []string, now time.Time) error {
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return fmt.Errorf("certificate is not currently valid")
	}
	for _, domain := range domains {
		if err := certificate.VerifyHostname(domain); err != nil {
			if len(certificate.DNSNames) != 0 || len(certificate.IPAddresses) != 0 || !legacyCommonNameMatches(certificate.Subject.CommonName, domain) {
				return fmt.Errorf("certificate does not match configured hostname %q", domain)
			}
		}
	}
	return nil
}

func legacyCommonNameMatches(commonName, hostname string) bool {
	commonName = strings.TrimSuffix(strings.TrimSpace(commonName), ".")
	hostname = strings.TrimSuffix(strings.TrimSpace(hostname), ".")
	if commonName == "" || hostname == "" {
		return false
	}
	if strings.EqualFold(commonName, hostname) {
		return true
	}
	if !strings.HasPrefix(commonName, "*.") {
		return false
	}
	suffix := commonName[1:]
	return len(hostname) > len(suffix) && strings.HasSuffix(strings.ToLower(hostname), strings.ToLower(suffix)) && strings.Count(hostname, ".") == strings.Count(commonName, ".")
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
	_, _ = w.Write([]byte(auth))
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
