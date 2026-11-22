package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDispatcherSignsGenericWebhook(t *testing.T) {
	const secret = "webhook-signing-secret"
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	var received *http.Request
	var receivedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		received = request.Clone(context.Background())
		received.Body = io.NopCloser(bytes.NewReader(body))
		receivedBody = body
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	dispatcher := NewDispatcher(Options{
		HTTPClient: client,
		UserAgent:  "gitignore/1.2.3 (+https://example.test)",
		Now:        func() time.Time { return now },
		RetryAfter: []time.Duration{},
		ResolveHost: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		},
	})
	event := Event{
		Role: "abuse", Event: "abuse.report_received", Subject: "Report",
		Body: "A report body", Severity: "informational", ProjectName: "gitignore",
		ProjectVersion: "1.2.3", AppURL: "https://example.test",
	}
	if failures := dispatcher.Dispatch(context.Background(), event, []Target{{
		Name: "generic", URL: "https://hooks.example.test/notify", Secret: secret,
	}}); len(failures) != 0 {
		t.Fatalf("Dispatch() failures = %v", failures)
	}
	if received == nil {
		t.Fatal("webhook request was not sent")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(receivedBody)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := received.Header.Get("X-Webhook-Signature"); got != wantSignature {
		t.Errorf("signature = %q, want %q", got, wantSignature)
	}
	if got := received.Header.Get("X-Webhook-Timestamp"); got != "1767323045" {
		t.Errorf("timestamp = %q, want fixed UTC timestamp", got)
	}
	if got := received.Header.Get("X-Webhook-Event"); got != event.Event {
		t.Errorf("event header = %q, want %q", got, event.Event)
	}
	if got := received.Header.Get("User-Agent"); got != "gitignore/1.2.3 (+https://example.test)" {
		t.Errorf("user agent = %q", got)
	}
	if got := received.Header.Get("X-Webhook-ID"); len(got) != 36 || got[14] != '7' {
		t.Errorf("webhook ID %q is not UUIDv7 shaped", got)
	}
	var gotEvent Event
	if err := json.Unmarshal(receivedBody, &gotEvent); err != nil {
		t.Fatalf("decode webhook event: %v", err)
	}
	if gotEvent.Role != event.Role || gotEvent.Body != event.Body {
		t.Errorf("event payload = %#v, want role %q and body %q", gotEvent, event.Role, event.Body)
	}
}

func TestValidateURLRejectsPrivateDNSResults(t *testing.T) {
	_, _, err := validateURL(context.Background(), "https://hooks.example.test/notify", func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3")}, nil
	})
	if err == nil {
		t.Fatal("validateURL() accepted a private DNS result")
	}
}

func TestValidateURLAcceptsOnlyPublicDNSResults(t *testing.T) {
	parsed, addresses, err := validateURL(context.Background(), "https://hooks.example.test/notify", func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	if err != nil {
		t.Fatalf("validateURL() rejected public DNS result: %v", err)
	}
	if parsed.Hostname() != "hooks.example.test" || len(addresses) != 1 || !addresses[0].Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("validated target = %s %v", parsed.Hostname(), addresses)
	}
}

func TestPublicWebhookIPRejectsReservedAndMappedAddresses(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "::1", "100.64.0.1", "192.0.2.1", "::ffff:10.0.0.1"} {
		if publicWebhookIP(net.ParseIP(value)) {
			t.Errorf("publicWebhookIP(%q) = true, want false", value)
		}
	}
}
