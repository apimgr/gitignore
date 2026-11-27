// Package email implements the AI.md PART 17 transactional email subsystem:
// SMTP transport (stdlib net/smtp), SMTP auto-detection, customizable templates
// with embedded defaults, and simple {variable} rendering. It has no dependency
// on the config package so the server and client binaries can wire it up without
// import cycles; the caller (package main) supplies SMTP settings as plain
// values.
package email

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// dialTimeout bounds every SMTP connection attempt so auto-detection cannot hang
// the startup path on a filtered port.
const dialTimeout = 3 * time.Second

// Sender carries the SMTP transport settings and envelope identity used to send
// operator notification emails. A zero Host means email is disabled.
type Sender struct {
	Host      string
	Port      int
	Username  string
	Password  string
	TLSMode   string
	FromName  string
	FromEmail string
	ReplyTo   string
}

// CanSend reports whether the sender is configured with an SMTP host. Per AI.md
// PART 17, no host means email is completely disabled — callers must not attempt
// to send.
func (s *Sender) CanSend() bool {
	return s != nil && strings.TrimSpace(s.Host) != "" && s.Port > 0
}

// addr returns the host:port dial target.
func (s *Sender) addr() string {
	return net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
}

// resolveTLSMode returns the effective TLS mode, resolving "auto" from the port:
// 465 uses implicit TLS, everything else uses STARTTLS when the server offers it.
func (s *Sender) resolveTLSMode() string {
	mode := strings.ToLower(strings.TrimSpace(s.TLSMode))
	if mode != "" && mode != "auto" {
		return mode
	}
	if s.Port == 465 {
		return "tls"
	}
	return "starttls"
}

func (s *Sender) configuredTLSMode() string {
	mode := strings.ToLower(strings.TrimSpace(s.TLSMode))
	if mode == "" {
		return "auto"
	}
	return mode
}

// PrivateTLSVerificationSkipped reports whether the configured host literal
// receives AI.md's private-address certificate verification exception.
func PrivateTLSVerificationSkipped(host string) bool {
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	shared := netip.MustParsePrefix("100.64.0.0/10")
	return a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || shared.Contains(a)
}

func (s *Sender) tlsConfig() *tls.Config {
	cfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	// AI.md permits this exception only for private IP literals configured by the operator.
	if PrivateTLSVerificationSkipped(s.Host) {
		cfg.InsecureSkipVerify = true
	}
	return cfg
}

// Send delivers a single plain-text message to one recipient. It returns an
// error if SMTP is not usable or delivery fails. It never queues or retries —
// AI.md PART 17 forbids queuing.
func (s *Sender) Send(to, subject, body string) error {
	return s.SendWithReplyTo(to, s.ReplyTo, subject, body)
}

// SendWithReplyTo delivers one message using a caller-specific reply address.
func (s *Sender) SendWithReplyTo(to, replyTo, subject, body string) error {
	return s.SendWithReplyToHTML(to, replyTo, subject, body, htmlFromPlain(body))
}

// SendHTML delivers a text and HTML alternative message to one recipient.
func (s *Sender) SendHTML(to, subject, plainBody, htmlBody string) error {
	return s.SendWithReplyToHTML(to, s.ReplyTo, subject, plainBody, htmlBody)
}

// SendWithReplyToHTML delivers a multipart message using a caller-specific reply address.
func (s *Sender) SendWithReplyToHTML(to, replyTo, subject, plainBody, htmlBody string) error {
	if !s.CanSend() {
		return errors.New("email: SMTP not configured")
	}
	if err := validateMessage(s, to, replyTo, subject, plainBody, htmlBody); err != nil {
		return err
	}

	client, encrypted, err := s.dial()
	if err != nil {
		return fmt.Errorf("email: connect: %w", err)
	}
	defer func() { _ = client.Close() }()

	if s.Username != "" {
		if !encrypted {
			return errors.New("email: SMTP AUTH requires TLS; credentials were not sent")
		}
		auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}

	if err := client.Mail(s.FromEmail); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	message, err := s.buildMessage(to, replyTo, subject, plainBody, htmlBody)
	if err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}
	return client.Quit()
}

// buildMessage assembles the RFC 5322 message with CRLF line endings.
func validateMessage(s *Sender, to, replyTo, subject, plainBody, htmlBody string) error {
	for label, value := range map[string]string{"recipient": to, "reply-to": replyTo, "subject": subject, "from name": s.FromName, "from email": s.FromEmail} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("email: invalid %s: CR/LF is not allowed", label)
		}
	}
	parsed, err := mail.ParseAddress(to)
	if err != nil || parsed.Address != to || strings.ContainsAny(to, " <>\t") {
		return errors.New("email: invalid recipient addr-spec")
	}
	from, err := mail.ParseAddress(s.FromEmail)
	if err != nil || from.Address != s.FromEmail || strings.ContainsAny(s.FromEmail, " <>\t") {
		return errors.New("email: invalid sender address")
	}
	if subject == "" {
		return errors.New("email: empty subject")
	}
	if strings.TrimSpace(plainBody) == "" {
		return errors.New("email: plain-text body must not be empty")
	}
	if replyTo != "" {
		if _, err := mail.ParseAddress(replyTo); err != nil {
			return errors.New("email: invalid reply-to address")
		}
	}
	if strings.TrimSpace(htmlBody) == "" {
		return errors.New("email: HTML body must not be empty")
	}
	return nil
}

func htmlFromPlain(body string) string { return "<pre>" + html.EscapeString(body) + "</pre>" }

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func (s *Sender) buildMessage(to, replyTo, subject, plainBody, htmlBody string) ([]byte, error) {
	var b bytes.Buffer
	boundarySeed := make([]byte, 18)
	if _, err := rand.Read(boundarySeed); err != nil {
		return nil, err
	}
	boundary := "alt-" + hex.EncodeToString(boundarySeed)
	messageIDSeed := make([]byte, 16)
	if _, err := rand.Read(messageIDSeed); err != nil {
		return nil, err
	}
	domain := s.FromEmail[strings.LastIndex(s.FromEmail, "@")+1:]
	from := (&mail.Address{Name: s.FromName, Address: s.FromEmail}).String()
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)
	if len([]byte(from)) > 900 || len([]byte(to)) > 900 || len([]byte(encodedSubject)) > 900 {
		return nil, errors.New("email: header exceeds safe line length")
	}
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\n", from, to)
	if replyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", (&mail.Address{Address: replyTo}).String())
	}
	fmt.Fprintf(&b, "Subject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nAuto-Submitted: auto-generated\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", encodedSubject, time.Now().Format(time.RFC1123Z), hex.EncodeToString(messageIDSeed), domain, boundary)
	mw := multipart.NewWriter(&b)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, err
	}
	for _, part := range []struct{ typ, body string }{{"text/plain", plainBody}, {"text/html", htmlBody}} {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Type", part.typ+"; charset=UTF-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		if _, err := io.WriteString(qp, normalizeCRLF(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// dial establishes an SMTP client using the resolved TLS mode.
func (s *Sender) dial() (*smtp.Client, bool, error) {
	mode := s.configuredTLSMode()
	if mode == "auto" && s.Port == 465 {
		mode = "tls"
	}
	switch mode {
	case "tls":
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", s.addr(), s.tlsConfig())
		if err != nil {
			return nil, false, err
		}
		client, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			_ = conn.Close()
			return nil, false, err
		}
		if err := client.Hello(localName()); err != nil {
			_ = client.Close()
			return nil, false, err
		}
		return client, true, nil
	case "none":
		return s.dialPlain(false, false)
	case "starttls":
		return s.dialPlain(true, true)
	case "auto":
		return s.dialPlain(true, false)
	default:
		return nil, false, fmt.Errorf("email: unknown TLS mode %q", mode)
	}
}

// dialPlain opens a plain TCP connection, performs the EHLO handshake, and
// optionally upgrades via STARTTLS when the server advertises it.
func (s *Sender) dialPlain(useSTARTTLS, requireSTARTTLS bool) (*smtp.Client, bool, error) {
	conn, err := net.DialTimeout("tcp", s.addr(), dialTimeout)
	if err != nil {
		return nil, false, err
	}
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if err := client.Hello(localName()); err != nil {
		_ = client.Close()
		return nil, false, err
	}
	if useSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(s.tlsConfig()); err != nil {
				_ = client.Close()
				return nil, false, err
			}
			return client, true, nil
		}
		if requireSTARTTLS {
			_ = client.Close()
			return nil, false, errors.New("email: STARTTLS required but not offered")
		}
	}
	return client, false, nil
}

// TestConnection attempts an SMTP handshake (EHLO) against host:port using the
// given TLS mode and returns an error if the server is unreachable or the
// handshake fails. It sends no mail.
func TestConnection(host string, port int, tlsMode string) error {
	probe := &Sender{Host: host, Port: port, TLSMode: tlsMode}
	client, _, err := probe.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.Quit()
}

// localName returns the EHLO client name. A resolvable hostname is preferred;
// localhost is a safe fallback.
func localName() string {
	if h, err := net.LookupCNAME("localhost"); err == nil && h != "" {
		return strings.TrimSuffix(h, ".")
	}
	return "localhost"
}

// detectPorts is the port priority list used during auto-detection.
var detectPorts = []int{25, 465, 587}

// DetectCandidate is one host tried during auto-detection, in priority order.
type DetectCandidate struct {
	Host string
	// Ports overrides detectPorts when non-empty (used by tests).
	Ports []int
}

// Detect walks the candidate hosts in priority order, attempts an SMTP handshake
// on each port (25, 465, 587), and returns the first working host:port. ok is
// false when no local SMTP server is reachable — this is not an error, it just
// means email stays disabled (AI.md PART 17 auto-detection).
func Detect(candidates []DetectCandidate) (host string, port int, ok bool) {
	for _, c := range candidates {
		if strings.TrimSpace(c.Host) == "" {
			continue
		}
		ports := c.Ports
		if len(ports) == 0 {
			ports = detectPorts
		}
		for _, p := range ports {
			mode := "auto"
			if p == 465 {
				mode = "tls"
			}
			if err := TestConnection(c.Host, p, mode); err == nil {
				return c.Host, p, true
			}
		}
	}
	return "", 0, false
}

// DefaultCandidates builds the AI.md PART 17 auto-detection priority list from
// the detected network identity. Empty inputs are skipped.
func DefaultCandidates(gatewayIP, fqdn, globalIPv4 string) []DetectCandidate {
	hosts := []string{
		"127.0.0.1",
		"172.17.0.1",
		gatewayIP,
		fqdn,
		globalIPv4,
	}
	if fqdn != "" {
		hosts = append(hosts, "mail."+fqdn, "smtp."+fqdn)
	}
	out := make([]DetectCandidate, 0, len(hosts))
	seen := map[string]bool{}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, DetectCandidate{Host: h})
	}
	return out
}
