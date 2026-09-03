package email

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
)

// mockSMTP is a minimal in-process SMTP server used to exercise Send and
// TestConnection without touching a real mail server or the network. It speaks
// just enough of the protocol (EHLO/MAIL/RCPT/DATA/QUIT) for net/smtp.
type mockSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	received strings.Builder
	from     string
	rcpt     string
}

func newMockSMTP(t *testing.T) *mockSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := &mockSMTP{ln: ln}
	go m.serve()
	return m
}

func (m *mockSMTP) addr() (host string, port int) {
	a := m.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (m *mockSMTP) close() { m.ln.Close() }

func (m *mockSMTP) serve() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return
		}
		go m.handle(conn)
	}
}

func (m *mockSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(s string) {
		w.WriteString(s + "\r\n")
		w.Flush()
	}
	writeLine("220 mock ESMTP ready")
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if inData {
			if trimmed == "." {
				inData = false
				writeLine("250 OK queued")
				continue
			}
			m.mu.Lock()
			m.received.WriteString(trimmed + "\n")
			m.mu.Unlock()
			continue
		}
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			writeLine("250-mock greets you")
			writeLine("250 SIZE 10485760")
		case strings.HasPrefix(upper, "MAIL FROM"):
			m.mu.Lock()
			m.from = trimmed
			m.mu.Unlock()
			writeLine("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			m.mu.Lock()
			m.rcpt = trimmed
			m.mu.Unlock()
			writeLine("250 OK")
		case upper == "DATA":
			inData = true
			writeLine("354 End data with <CR><LF>.<CR><LF>")
		case upper == "QUIT":
			writeLine("221 Bye")
			return
		default:
			writeLine("250 OK")
		}
	}
}

func (m *mockSMTP) body() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.received.String()
}

func TestSendDeliversMessage(t *testing.T) {
	m := newMockSMTP(t)
	defer m.close()
	host, port := m.addr()

	s := &Sender{
		Host:      host,
		Port:      port,
		TLSMode:   "none",
		FromName:  "gitignore",
		FromEmail: "no-reply@example.com",
		ReplyTo:   "ops@example.com",
	}
	if !s.CanSend() {
		t.Fatal("CanSend should be true with host and port set")
	}
	if err := s.Send("admin@example.com", "Hello", "Line one\nLine two"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := m.body()
	for _, want := range []string{
		"From: gitignore <no-reply@example.com>",
		"To: admin@example.com",
		"Reply-To: ops@example.com",
		"Subject: Hello",
		"Line one",
		"Line two",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("delivered message missing %q\n---\n%s", want, got)
		}
	}
}

func TestTestConnectionHandshake(t *testing.T) {
	m := newMockSMTP(t)
	defer m.close()
	host, port := m.addr()
	if err := TestConnection(host, port, "none"); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestDetectFindsMockServer(t *testing.T) {
	m := newMockSMTP(t)
	defer m.close()
	host, port := m.addr()
	cands := []DetectCandidate{
		{Host: "127.0.0.1", Ports: []int{port}},
	}
	gotHost, gotPort, ok := Detect(cands)
	if !ok {
		t.Fatal("Detect should find the mock server")
	}
	if gotHost != host || gotPort != port {
		t.Errorf("Detect = %s:%d, want %s:%d", gotHost, gotPort, host, port)
	}
}

func TestDetectNoServer(t *testing.T) {
	// Bind a port then close it so nothing is listening there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	_, _, ok := Detect([]DetectCandidate{{Host: "127.0.0.1", Ports: []int{port}}})
	if ok {
		t.Error("Detect should report ok=false when no server is listening")
	}
}

func TestCanSendFalseWithoutHost(t *testing.T) {
	if (&Sender{Port: 587}).CanSend() {
		t.Error("CanSend must be false without a host")
	}
	var nilSender *Sender
	if nilSender.CanSend() {
		t.Error("nil Sender CanSend must be false")
	}
}

func TestResolveTLSMode(t *testing.T) {
	cases := []struct {
		mode string
		port int
		want string
	}{
		{"auto", 465, "tls"},
		{"auto", 587, "starttls"},
		{"", 25, "starttls"},
		{"starttls", 465, "starttls"},
		{"none", 587, "none"},
		{"TLS", 25, "tls"},
	}
	for _, c := range cases {
		s := &Sender{TLSMode: c.mode, Port: c.port}
		if got := s.resolveTLSMode(); got != c.want {
			t.Errorf("resolveTLSMode(%q, %d) = %q, want %q", c.mode, c.port, got, c.want)
		}
	}
}

func TestDefaultCandidatesOrderAndDedup(t *testing.T) {
	c := DefaultCandidates("192.168.1.1", "example.com", "203.0.113.5")
	if len(c) < 5 {
		t.Fatalf("expected at least 5 candidates, got %d", len(c))
	}
	if c[0].Host != "127.0.0.1" || c[1].Host != "172.17.0.1" {
		t.Errorf("priority order wrong: %s, %s", c[0].Host, c[1].Host)
	}
	var hasMail, hasSMTP bool
	for _, cand := range c {
		if cand.Host == "mail.example.com" {
			hasMail = true
		}
		if cand.Host == "smtp.example.com" {
			hasSMTP = true
		}
	}
	if !hasMail || !hasSMTP {
		t.Error("expected mail. and smtp. subdomain candidates")
	}
}
