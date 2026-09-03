package email

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSubstitutesVariables(t *testing.T) {
	r := NewRenderer("")
	vars := map[string]string{
		"app_name":  "gitignore",
		"app_url":   "https://api.example.com",
		"fqdn":      "api.example.com",
		"timestamp": "2026-07-20T00:00:00Z",
		"filename":  "backup-2026.tar.zst",
		"size":      "12 MB",
	}
	subject, body, err := r.Render("backup_complete", vars)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if subject != "Backup Complete - gitignore" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"backup-2026.tar.zst", "12 MB", "https://api.example.com", "api.example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "{filename}") {
		t.Error("unsubstituted {filename} left in body")
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	if _, _, err := NewRenderer("").Render("does_not_exist", nil); err == nil {
		t.Error("expected error for unknown template")
	}
}

func TestCustomOverrideTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	custom := "Subject: Custom {app_name}\n---\nOverride body {fqdn}\n"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRenderer(dir)
	subject, body, err := r.Render("test", map[string]string{"app_name": "X", "fqdn": "h"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if subject != "Custom X" {
		t.Errorf("subject = %q, want custom override", subject)
	}
	if !strings.Contains(body, "Override body h") {
		t.Errorf("body = %q, want custom override", body)
	}
}

func TestValidateTemplateErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"no subject", "Hello\n---\nbody", "Subject:"},
		{"no separator", "Subject: Hi\nbody with no sep", "separator"},
		{"empty subject", "Subject: \n---\nbody", "Subject cannot be empty"},
		{"empty body", "Subject: Hi\n---\n   \n", "Body cannot be empty"},
		{"unknown var", "Subject: Hi\n---\nbody {bogus}", "Unknown variable: {bogus}"},
	}
	for _, c := range cases {
		err := ValidateTemplate(c.raw)
		if err == nil {
			t.Errorf("%s: expected error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %q, want substring %q", c.name, err.Error(), c.want)
		}
	}
}

func TestValidateTemplateValid(t *testing.T) {
	raw := "Subject: Hi {app_name}\n---\nBody {fqdn} {timestamp}\n"
	if err := ValidateTemplate(raw); err != nil {
		t.Errorf("valid template rejected: %v", err)
	}
}

func TestAllEmbeddedDefaultsValidate(t *testing.T) {
	r := NewRenderer("")
	if err := ValidateAll(r); err != nil {
		t.Fatalf("embedded defaults invalid: %v", err)
	}
	// Every declared template must render without error.
	for _, name := range TemplateNames {
		if _, _, err := r.Render(name, map[string]string{}); err != nil {
			t.Errorf("Render(%q): %v", name, err)
		}
	}
}
