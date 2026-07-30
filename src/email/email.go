// Package email implements the AI.md PART 17 transactional email subsystem:
// SMTP transport (stdlib net/smtp), SMTP auto-detection, customizable templates
// with embedded defaults, and simple {variable} rendering. It has no dependency
// on the config package so the server and client binaries can wire it up without
// import cycles; the caller (package main) supplies SMTP settings as plain
// values.
package email

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
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

// fromHeader returns the RFC 5322 From header value.
func (s *Sender) fromHeader() string {
	if s.FromName != "" {
		return fmt.Sprintf("%s <%s>", s.FromName, s.FromEmail)
	}
	return s.FromEmail
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

// Send delivers a single plain-text message to one recipient. It returns an
// error if SMTP is not usable or delivery fails. It never queues or retries —
// AI.md PART 17 forbids queuing.
func (s *Sender) Send(to, subject, body string) error {
	if !s.CanSend() {
		return errors.New("email: SMTP not configured")
	}
	if strings.TrimSpace(to) == "" {
		return errors.New("email: empty recipient")
	}

	client, err := s.dial()
	if err != nil {
		return fmt.Errorf("email: connect: %w", err)
	}
	defer client.Close()

	if s.Username != "" {
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
	if _, err := w.Write(s.buildMessage(to, subject, body)); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}
	return client.Quit()
}

// buildMessage assembles the RFC 5322 message with CRLF line endings.
func (s *Sender) buildMessage(to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + s.fromHeader() + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	if s.ReplyTo != "" {
		b.WriteString("Reply-To: " + s.ReplyTo + "\r\n")
	}
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	// Normalize to CRLF and dot-stuff is handled by net/smtp's DataWriter.
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}

// dial establishes an SMTP client using the resolved TLS mode.
func (s *Sender) dial() (*smtp.Client, error) {
	switch s.resolveTLSMode() {
	case "tls":
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", s.addr(), &tls.Config{ServerName: s.Host})
		if err != nil {
			return nil, err
		}
		client, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if err := client.Hello(localName()); err != nil {
			client.Close()
			return nil, err
		}
		return client, nil
	case "none":
		return s.dialPlain(false)
	default:
		// starttls
		return s.dialPlain(true)
	}
}

// dialPlain opens a plain TCP connection, performs the EHLO handshake, and
// optionally upgrades via STARTTLS when the server advertises it.
func (s *Sender) dialPlain(useSTARTTLS bool) (*smtp.Client, error) {
	conn, err := net.DialTimeout("tcp", s.addr(), dialTimeout)
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := client.Hello(localName()); err != nil {
		client.Close()
		return nil, err
	}
	if useSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				client.Close()
				return nil, err
			}
		}
	}
	return client, nil
}

// TestConnection attempts an SMTP handshake (EHLO) against host:port using the
// given TLS mode and returns an error if the server is unreachable or the
// handshake fails. It sends no mail.
func TestConnection(host string, port int, tlsMode string) error {
	probe := &Sender{Host: host, Port: port, TLSMode: tlsMode}
	client, err := probe.dial()
	if err != nil {
		return err
	}
	defer client.Close()
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
			mode := "starttls"
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
