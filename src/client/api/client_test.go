package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserAgent(t *testing.T) {
	origProject, origVersion := ProjectName, Version
	t.Cleanup(func() { ProjectName, Version = origProject, origVersion })

	ProjectName, Version = "gitignore", "1.2.3"
	if got, want := UserAgent(), "gitignore-cli/1.2.3"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := New("https://example.com/")
	if c.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q, want trimmed", c.BaseURL)
	}
	if c.HTTPClient == nil {
		t.Error("expected non-nil HTTPClient")
	}
}

func TestAPIErrorError(t *testing.T) {
	e := &APIError{Status: 404, Message: "not found"}
	if got := e.Error(); got != "server returned 404: not found" {
		t.Errorf("Error() = %q", got)
	}
}

func TestListSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/list" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept header = %q, want application/json", got)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"data": []string{"Go", "Node"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	names, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 || names[0] != "Go" || names[1] != "Node" {
		t.Errorf("List() = %v, want [Go Node]", names)
	}
}

func TestListServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "boom",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.List()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", apiErr.Status)
	}
	if apiErr.Message != "boom" {
		t.Errorf("Message = %q, want boom", apiErr.Message)
	}
}

func TestListServerErrorNoBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.List()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Message == "" {
		t.Error("expected a non-empty fallback message (HTTP status text)")
	}
}

func TestSearchCategoriesCategoryTemplates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			if got := r.URL.Query().Get("q"); got != "go" {
				t.Errorf("query q = %q, want go", got)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{"Go"}})
		case "/api/v1/categories":
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{"Root", "Global"}})
		case "/api/v1/categories/Root":
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{"Go", "Rust"}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL)

	if got, err := c.Search("go"); err != nil || len(got) != 1 || got[0] != "Go" {
		t.Errorf("Search() = (%v, %v), want ([Go], nil)", got, err)
	}
	if got, err := c.Categories(); err != nil || len(got) != 2 {
		t.Errorf("Categories() = (%v, %v)", got, err)
	}
	if got, err := c.CategoryTemplates("Root"); err != nil || len(got) != 2 {
		t.Errorf("CategoryTemplates() = (%v, %v)", got, err)
	}
}

func TestGetTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/templates/Go" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"data": Template{
				Name: "Go", FileName: "Go.gitignore", Category: "Root", Content: "*.o\n",
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	tmpl, err := c.GetTemplate("Go")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if tmpl.Name != "Go" || tmpl.Content != "*.o\n" {
		t.Errorf("GetTemplate() = %+v", tmpl)
	}
}

func TestCombine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("templates"); got != "Go,Node" {
			t.Errorf("templates query = %q, want Go,Node", got)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": "*.o\nnode_modules/\n"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	content, err := c.Combine([]string{"Go", "Node"})
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if content != "*.o\nnode_modules/\n" {
		t.Errorf("Combine() = %q", content)
	}
}

func TestStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"data": map[string]interface{}{"total_templates": float64(42)},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	stats, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats["total_templates"] != float64(42) {
		t.Errorf("Stats() = %v", stats)
	}
}

func TestHealthz(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}))
		defer srv.Close()
		if err := New(srv.URL).Healthz(); err != nil {
			t.Errorf("Healthz() = %v, want nil", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		c := New("http://127.0.0.1:1")
		if err := c.Healthz(); err == nil {
			t.Error("expected error for unreachable server")
		}
	})
}

func TestGetInvalidBaseURL(t *testing.T) {
	c := New("://not-a-url")
	if _, err := c.List(); err == nil {
		t.Error("expected error for invalid base URL")
	}
}

func TestAcceptLanguageHeaderSentWhenLangSet(t *testing.T) {
	var gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = r.Header.Get("Accept-Language")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": []string{}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Lang = "de"
	if _, err := c.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotLang != "de" {
		t.Errorf("Accept-Language = %q, want de", gotLang)
	}
}

func TestStatsCount(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{float64(42), "42"},
		{float64(3.5), "3.5"},
		{"already-a-string", "already-a-string"},
		{nil, "<nil>"},
	}
	for _, tc := range cases {
		if got := StatsCount(tc.in); got != tc.want {
			t.Errorf("StatsCount(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
