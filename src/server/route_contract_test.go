package server

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/template"
	"github.com/go-chi/chi/v5"
)

var contractParameter = regexp.MustCompile(`\{[^{}]+\}`)

func TestProjectAPIRouteContract(t *testing.T) {
	idea, err := os.Open("../../IDEA.md")
	if err != nil {
		t.Fatalf("open IDEA.md: %v", err)
	}
	t.Cleanup(func() {
		if err := idea.Close(); err != nil {
			t.Errorf("close IDEA.md: %v", err)
		}
	})

	want := make(map[string]struct{})
	scanner := bufio.NewScanner(idea)
	inRoutes := false
	tableStarted := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "### API routes" {
			inRoutes = true
			continue
		}
		if inRoutes && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		if !inRoutes {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			if tableStarted && strings.TrimSpace(line) != "" {
				break
			}
			continue
		}
		tableStarted = true
		if strings.HasPrefix(line, "| Method") || strings.HasPrefix(line, "|--------") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		methods := strings.Split(strings.TrimSpace(cells[0]), ",")
		path := strings.Trim(strings.TrimSpace(cells[1]), "`")
		if query := strings.IndexByte(path, '?'); query >= 0 {
			path = path[:query]
		}
		if !strings.HasPrefix(path, "/") {
			continue
		}
		path = strings.ReplaceAll(path, "{api_version}", "v1")
		path = contractParameter.ReplaceAllString(path, "{}")
		for _, method := range methods {
			want[strings.ToUpper(strings.TrimSpace(method))+" "+path] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read IDEA.md: %v", err)
	}
	if !inRoutes {
		t.Fatal("IDEA.md has no ### API routes subsection")
	}
	if len(want) == 0 {
		t.Fatal("IDEA.md ### API routes subsection contains no route inventory")
	}

	ai, err := os.Open("../../AI.md")
	if err != nil {
		t.Fatalf("open AI.md: %v", err)
	}
	t.Cleanup(func() {
		if err := ai.Close(); err != nil {
			t.Errorf("close AI.md: %v", err)
		}
	})
	scanner = bufio.NewScanner(ai)
	inRootEndpoints := false
	tableStarted = false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "## Root-Level Endpoints" {
			inRootEndpoints = true
			continue
		}
		if inRootEndpoints && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		if !inRootEndpoints {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			if tableStarted && strings.TrimSpace(line) != "" {
				break
			}
			continue
		}
		tableStarted = true
		if strings.HasPrefix(line, "| Endpoint") || strings.HasPrefix(line, "|----------") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		path := strings.Trim(strings.TrimSpace(cells[0]), "`")
		if !strings.HasPrefix(path, "/api/") || strings.ContainsAny(path, "[]*") {
			continue
		}
		path = strings.ReplaceAll(path, "{api_version}", "v1")
		path = contractParameter.ReplaceAllString(path, "{}")
		for _, method := range strings.Split(strings.TrimSpace(cells[1]), ",") {
			want[strings.ToUpper(strings.TrimSpace(method))+" "+path] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read AI.md: %v", err)
	}
	if !inRootEndpoints {
		t.Fatal("AI.md has no ## Root-Level Endpoints section")
	}

	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		config: &Config{Templates: templates, Cfg: config.DefaultConfig()},
		router: chi.NewRouter(),
	}
	server.setupRoutes()

	registered := make(map[string]struct{})
	if err := chi.Walk(server.router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+contractParameter.ReplaceAllString(path, "{}")] = struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("walk router: %v", err)
	}

	openAPI, ok := server.openAPISpec(httptest.NewRequest("GET", "http://example.test/", nil))["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("generated OpenAPI document has no paths map")
	}
	generated := make(map[string]struct{})
	for path, raw := range openAPI {
		operations, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		for method := range operations {
			if method == "parameters" || method == "summary" {
				continue
			}
			generated[strings.ToUpper(method)+" "+contractParameter.ReplaceAllString(path, "{}")] = struct{}{}
		}
	}

	for route := range want {
		if _, ok := registered[route]; !ok {
			t.Errorf("IDEA.md route %q is missing from the registered router", route)
		}
		if _, ok := generated[route]; !ok {
			t.Errorf("IDEA.md route %q is missing from generated OpenAPI", route)
		}
	}
}

func TestHubPageRoutesAreRegisteredOrActions(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{config: &Config{Templates: templates, Cfg: config.DefaultConfig()}, router: chi.NewRouter()}
	server.setupRoutes()
	registered := make(map[string]bool, len(serverPageRegistry))
	for _, page := range serverPageRegistry {
		registered[page.path] = true
	}
	if err := chi.Walk(server.router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			return nil
		}
		for _, hub := range []string{"/server/preferences", "/server/help"} {
			prefix := hub + "/"
			if !strings.HasPrefix(path, prefix) || strings.Contains(strings.TrimPrefix(path, prefix), "/") {
				continue
			}
			if !registered[path] {
				t.Errorf("GET hub child %s is not registered as a page", path)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk router: %v", err)
	}
}
