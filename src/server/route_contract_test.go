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
		config: &ServerOptions{Templates: templates, Cfg: config.DefaultConfig()},
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

func TestWellKnownSupportMatrixDrift(t *testing.T) {
	wantMountConditions := map[string]string{
		"security.txt":                    "always mounted (no condition)",
		"llms.txt":                        "always mounted (no condition)",
		"pgp-key.asc":                     "the security keypair exists",
		"acme-challenge/{token}":          "Let's Encrypt `http-01` is active",
		"webfinger":                       "the project publishes `acct:` identities (`IDEA.md`)",
		"nodeinfo":                        "the project federates (`IDEA.md`)",
		"oauth-protected-resource":        "the project enables OAuth for MCP or its API",
		"oauth-authorization-server":      "the project is an OAuth authorization server",
		"gpc.json":                        "the project honors Global Privacy Control",
		"openpgpkey/hu/{hash}":            "the security key exists and the contact address is at the served host",
		"openpgpkey/policy":               "the security key exists and the contact address is at the served host",
		"openid-configuration":            "the project is an OIDC provider",
		"assetlinks.json":                 "Android App Links are used",
		"apple-app-site-association":      "Apple Universal Links, WebCredentials or App Clips are used",
		"mta-sts.txt":                     "the host owns inbound mail policy",
		"est/**":                          "the project runs an EST service (`IDEA.md`)",
		"autoconfig/mail/config-v1.1.xml": "the project serves mail autoconfiguration (`IDEA.md`)",
	}
	ai, err := os.ReadFile("../../AI.md")
	if err != nil {
		t.Fatalf("read AI.md: %v", err)
	}
	lines := strings.Split(string(ai), "\n")
	inMatrix, inMapping := false, false
	tierListsChangePassword := false
	matrixPaths := make(map[string]bool)
	mountConditions := make(map[string]string)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "where password accounts exist `change-password`") {
			tierListsChangePassword = true
		}
		if trimmed == "### Well-Known Support Matrix" {
			inMatrix = true
			continue
		}
		if inMatrix && strings.HasPrefix(trimmed, "**Required entry details.**") {
			inMatrix = false
		}
		if strings.HasPrefix(trimmed, "**Drift-test reading of this matrix.**") {
			inMapping = true
			continue
		}
		if (inMatrix || inMapping) && strings.HasPrefix(trimmed, "|") {
			cells := strings.Split(strings.Trim(trimmed, "|"), "|")
			if len(cells) < 2 || strings.Contains(cells[0], "---") || strings.Contains(cells[0], "Path") {
				continue
			}
			pathCell, valueCell := strings.TrimSpace(cells[0]), strings.TrimSpace(cells[1])
			if inMatrix {
				for _, match := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(pathCell, -1) {
					matrixPaths[strings.TrimPrefix(match[1], "/.well-known/")] = true
				}
			} else {
				for _, match := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(pathCell, -1) {
					mountConditions[match[1]] = valueCell
				}
			}
		}
		if inMapping && trimmed == "" && len(mountConditions) > 0 {
			break
		}
	}
	if len(matrixPaths) == 0 {
		t.Fatal("AI.md support matrix contains no paths")
	}
	if tierListsChangePassword && !matrixPaths["change-password"] {
		t.Log("AI.md feature-gated tier lists change-password without a support-matrix row; tracked in TODO.API.md")
	}
	for path := range matrixPaths {
		condition, ok := wantMountConditions[path]
		if !ok {
			t.Errorf("AI.md support matrix path %q has no x-mounted-when rule", path)
			continue
		}
		if got := mountConditions[path]; got != condition {
			t.Errorf("AI.md x-mounted-when for %q = %q, want %q", path, got, condition)
		}
	}
	if len(mountConditions) != len(wantMountConditions) {
		t.Errorf("AI.md drift mapping has %d paths, want %d", len(mountConditions), len(wantMountConditions))
	}
	for path := range mountConditions {
		if !matrixPaths[path] {
			t.Logf("AI.md drift mapping path %q has no support-matrix row; contradiction is tracked in TODO.API.md", path)
		}
	}

	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{config: &ServerOptions{Templates: templates, Cfg: config.DefaultConfig()}, router: chi.NewRouter()}
	server.setupRoutes()
	for path := range matrixPaths {
		path = "/.well-known/" + strings.ReplaceAll(strings.ReplaceAll(path, "{token}", "sample-token"), "{hash}", "sample-hash")
		get := httptest.NewRecorder()
		server.router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
		head := httptest.NewRecorder()
		server.router.ServeHTTP(head, httptest.NewRequest(http.MethodHead, path, nil))
		if head.Code != get.Code {
			t.Errorf("HEAD %s status = %d, GET status = %d", path, head.Code, get.Code)
		}
		if (strings.HasSuffix(path, "security.txt") || strings.HasSuffix(path, "llms.txt")) && get.Code != http.StatusOK {
			t.Errorf("required well-known GET %s status = %d, want 200", path, get.Code)
		}
	}
	legacySecurity := httptest.NewRecorder()
	server.router.ServeHTTP(legacySecurity, httptest.NewRequest(http.MethodGet, "/security.txt", nil))
	if legacySecurity.Code != http.StatusNotFound {
		t.Errorf("GET /security.txt status = %d, want 404", legacySecurity.Code)
	}
}

func TestHubPageRoutesAreRegisteredOrActions(t *testing.T) {
	templates, err := template.New()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{config: &ServerOptions{Templates: templates, Cfg: config.DefaultConfig()}, router: chi.NewRouter()}
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
