package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/notify"
)

type webhookRoundTripFunc func(*http.Request) (*http.Response, error)

func (f webhookRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestContactWebhookTargetsUsesPerTransportFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Contact.Admin.Webhooks = map[string]string{
		"telegram":        "https://admin.example.test/telegram",
		"telegram_secret": "admin-telegram-secret",
		"generic":         "https://admin.example.test/generic",
		"generic_secret":  "admin-generic-secret",
	}
	cfg.Server.Contact.Security.Webhooks = map[string]string{
		"telegram":        "https://security.example.test/telegram",
		"telegram_secret": "security-telegram-secret",
	}
	s := New(&Config{Cfg: cfg})

	targets := s.contactWebhookTargets("security")
	got := make(map[string]notify.Target, len(targets))
	for _, target := range targets {
		got[target.Name] = target
	}
	if len(got) != 2 {
		t.Fatalf("security targets = %d, want security telegram and admin generic", len(got))
	}
	if target := got["telegram"]; target.URL != "https://security.example.test/telegram" || target.Secret != "security-telegram-secret" {
		t.Errorf("security telegram target = %#v", target)
	}
	if target := got["generic"]; target.URL != "https://admin.example.test/generic" || target.Secret != "admin-generic-secret" {
		t.Errorf("admin generic fallback target = %#v", target)
	}
}

func TestContactWebhookTargetsAndDispatchSupportWebhookOnlyPrivacy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Contact.General.Webhooks = map[string]string{
		"generic":        "https://general.example.test/privacy",
		"generic_secret": "general-secret",
	}
	dispatched := make(chan notify.Event, 1)
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		var event notify.Event
		if err := json.Unmarshal(body, &event); err != nil {
			return nil, err
		}
		dispatched <- event
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	cfg.Server.Branding.Title = "GitIgnore API"
	s := New(&Config{
		Cfg: cfg, Version: "1.2.3",
		WebhookDispatcher: notify.NewDispatcher(notify.Options{
			HTTPClient: client, RetryAfter: []time.Duration{},
			ResolveHost: func(context.Context, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("8.8.8.8")}, nil
			},
		}),
	})
	if cfg.Server.Contact.DPO.Email != "" {
		t.Fatalf("default DPO email is unexpectedly set: %q", cfg.Server.Contact.DPO.Email)
	}
	if targets := s.contactWebhookTargets("privacy"); len(targets) != 1 {
		t.Fatalf("privacy webhook targets = %d, want one despite no email recipient", len(targets))
	}
	s.dispatchContactWebhook(httptest.NewRequest(http.MethodPost, "https://example.test/server/contact", nil), "privacy", "Privacy report", "privacy request", "informational", "")
	select {
	case event := <-dispatched:
		if event.Role != "general" || event.Event != "general.report_received" {
			t.Errorf("privacy event role/type = %q/%q, want general/general.report_received", event.Role, event.Event)
		}
		if event.ProjectName != "gitignore" || event.ProjectVersion != "1.2.3" {
			t.Errorf("project metadata = %q/%q", event.ProjectName, event.ProjectVersion)
		}
		if event.Body != "privacy request" {
			t.Errorf("event body = %q", event.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("webhook dispatch did not happen asynchronously")
	}
}

func TestContactWebhookTargetsDoNotUseGeneralForConfiguredDPO(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Contact.DPO.Email = "dpo@example.test"
	cfg.Server.Contact.General.Webhooks = map[string]string{
		"generic":        "https://general.example.test/privacy",
		"generic_secret": "general-secret",
	}
	s := New(&Config{Cfg: cfg})
	if targets := s.contactWebhookTargets("privacy"); len(targets) != 0 {
		t.Fatalf("privacy webhook targets with DPO email = %d, want none", len(targets))
	}
}

func TestVersionedContactErrorsUseAPIEnvelope(t *testing.T) {
	cfg := config.DefaultConfig()
	s := New(&Config{Cfg: cfg})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/contact", nil)
	recorder := httptest.NewRecorder()
	s.renderContact(recorder, request, http.StatusBadRequest, map[string]interface{}{"error": "Invalid contact form."})
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want JSON", got)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response["ok"] != false || response["error"] != "VALIDATION_FAILED" || response["message"] != "Invalid contact form." || response["details"] == nil {
		t.Fatalf("API error response = %#v", response)
	}
	if strings.Contains(recorder.Body.String(), "<!doctype html") {
		t.Fatal("versioned contact error rendered HTML")
	}
}
