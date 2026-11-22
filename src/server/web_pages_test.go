package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/html"
)

// newTestPageServer builds a minimal Server sufficient for rendering static
// standard pages and error pages (no Templates manager needed).
func newTestPageServer() *Server {
	return &Server{config: &Config{Version: "test", Cfg: config.DefaultConfig()}, router: chi.NewRouter()}
}

func TestContactFormSendsOnceAfterCaptcha(t *testing.T) {
	if err := db.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := newTestPageServer()
	s.config.Cfg.Server.Contact.Abuse.Email = "abuse@example.test"
	var gotTo, gotSubject, gotBody string
	s.config.SendContactReply = func(to, replyTo, subject, body string) error {
		gotTo, gotSubject, gotBody = to, subject, body
		if replyTo != "ada@example.test" {
			t.Errorf("Reply-To = %q", replyTo)
		}
		return nil
	}
	challengeReq := httptest.NewRequest(http.MethodGet, "/server/contact?type=abuse", nil)
	challengeID, _, err := s.issueContactChallenge(challengeReq)
	if err != nil {
		t.Fatal(err)
	}
	formToken, err := issueContactFormToken(time.Now().Add(-3 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"type": {"abuse"}, "name": {"Ada Lovelace"}, "email": {"ada@example.test"},
		"reported_url": {"https://gitignore.example/server/about"}, "reason": {"terms_violation"},
		"subject": {"Report"}, "message": {"Please review this page."},
		"captcha_id": {challengeID}, "captcha_answer": {challengeAnswer(t, s, challengeID)}, "form_token": {formToken},
		"step": {"submit"},
	}
	postContactForm(t, s, form)
	if gotTo != "" {
		t.Fatalf("first step sent an email to %q", gotTo)
	}

	confirmed := cloneContactValues(form)
	confirmed.Set("step", "confirm")
	confirmed.Set("confirmed", "yes")
	reviewToken, err := s.issueReviewToken(confirmed, time.Now().Add(-3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	confirmed.Set("review_token", reviewToken)
	rec := postContactForm(t, s, confirmed)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("confirm status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if gotTo != "abuse@example.test" || gotSubject != "[gitignore contact] Report" || !strings.Contains(gotBody, "Please review this page.") {
		t.Fatalf("unexpected mail delivery: to=%q subject=%q body=%q", gotTo, gotSubject, gotBody)
	}
}

func challengeAnswer(t *testing.T, s *Server, id string) string {
	t.Helper()
	s.contactMu.Lock()
	challenge := s.contactChallenges[id]
	s.contactMu.Unlock()
	for answer := 0; answer <= 40; answer++ {
		if hashContactAnswer(id, strconv.Itoa(answer)) == challenge.answerHash {
			return strconv.Itoa(answer)
		}
	}
	t.Fatal("challenge answer not found")
	return ""
}

func cloneContactValues(values url.Values) url.Values {
	copy := make(url.Values, len(values))
	for key, entries := range values {
		copy[key] = append([]string(nil), entries...)
	}
	return copy
}

func postContactForm(t *testing.T, s *Server, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/server/contact", strings.NewReader(values.Encode()))
	request.Host = "gitignore.example"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://gitignore.example")
	recorder := httptest.NewRecorder()
	s.handleContactSubmit(recorder, request)
	return recorder
}

// doPage issues a GET against a page handler with an optional theme cookie.
func doPage(h http.HandlerFunc, path, themeCookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if themeCookie != "" {
		req.AddCookie(&http.Cookie{Name: "theme", Value: themeCookie})
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// TestStandardPagesRender verifies every mandated standard page returns 200,
// HTML, and content that identifies the page (AI.md PART 16 "Standard pages").
func TestStandardPagesRender(t *testing.T) {
	s := newTestPageServer()
	cases := []struct {
		path    string
		handler http.HandlerFunc
		needle  string
	}{
		{"/server", s.handleServerPage, "Server"},
		{"/server/about", s.handleAboutPage, "About"},
		{"/server/privacy", s.handlePrivacyPage, "Privacy"},
		{"/server/contact", s.handleContactPage, "Contact"},
		{"/server/help", s.handleHelpPage, "Help"},
		{"/server/help/mcp", s.handleMCPHelpPage, "MCP server"},
		{"/server/terms", s.handleTermsPage, "Terms of Service"},
		{"/server/security", s.handleSecurityPage, "private vulnerability reporting"},
		{"/server/security/policy", s.handleSecurityPolicyPage, "Security Disclosure Policy"},
		{"/server/security/thanks", s.handleSecurityThanksPage, "No acknowledgments are currently published"},
	}
	for _, c := range cases {
		rec := doPage(c.handler, c.path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", c.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: expected text/html, got %q", c.path, ct)
		}
		if !strings.Contains(rec.Body.String(), c.needle) {
			t.Errorf("%s: body missing %q", c.path, c.needle)
		}
	}
}

func TestRequestPathUsesConfiguredAndTrustedProxyPrefixes(t *testing.T) {
	s := newTestPageServer()
	s.config.Cfg.Server.BaseURL = "/configured/"
	s.trustedProxies = buildTrustedProxies("", []string{"203.0.113.9"})

	configured := httptest.NewRequest(http.MethodGet, "/server/about", nil)
	if got := s.requestPath(configured, "/server/about"); got != "/configured/server/about" {
		t.Fatalf("configured prefix path = %q", got)
	}

	trusted := httptest.NewRequest(http.MethodGet, "/server/about", nil)
	trusted.RemoteAddr = "203.0.113.9:8080"
	trusted.Header.Set("X-Forwarded-Prefix", "/proxy")
	trusted.Header.Set("X-Forwarded-Path", "/ignored")
	if got := s.requestPath(trusted, "/server/about"); got != "/proxy/server/about" {
		t.Fatalf("trusted proxy prefix path = %q", got)
	}
	response := httptest.NewRecorder()
	s.renderPage(response, trusted, "about", PageData{Title: "About"})
	for _, want := range []string{`href="/proxy/static/css/main.css"`, `href="/proxy/server/preferences"`, `src="/proxy/static/js/app.js"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("prefixed page is missing %q", want)
		}
	}

	untrusted := httptest.NewRequest(http.MethodGet, "/server/about", nil)
	untrusted.RemoteAddr = "198.51.100.8:8080"
	untrusted.Header.Set("X-Forwarded-Prefix", "/attacker")
	if got := s.requestPath(untrusted, "/server/about"); got != "/configured/server/about" {
		t.Fatalf("untrusted proxy prefix path = %q", got)
	}
	for _, header := range []struct {
		name string
		want string
	}{
		{"X-Forwarded-Path", "/forwarded-path"},
		{"X-Script-Name", "/script-name"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/server/about", nil)
		request.RemoteAddr = "203.0.113.9:8080"
		request.Header.Set(header.name, header.want)
		if got := s.requestPath(request, "/server/about"); got != header.want+"/server/about" {
			t.Errorf("%s prefix path = %q", header.name, got)
		}
	}

	for _, invalid := range []string{"https://evil.example", "//evil.example", "/safe/../escape", "/prefix?next=/evil", "/prefix\\evil", "/prefix//evil"} {
		if got := normalizePathPrefix(invalid); got != "" {
			t.Errorf("normalizePathPrefix(%q) = %q, want empty", invalid, got)
		}
	}
}

func TestServerPageShellConformance(t *testing.T) {
	s := newTestPageServer()
	pages := []string{
		"server", "about", "privacy", "contact", "help", "mcp_help", "preferences",
		"preferences_sync", "preferences_tokens", "terms", "dpo", "security",
		"security_policy", "security_thanks", "security_report_status", "error",
		"swagger_ui", "graphql_ui", "healthz",
	}
	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			pagePath := map[string]string{"security_policy": "/server/security/policy", "security_thanks": "/server/security/thanks", "security_report_status": "/server/security/report/test"}[page]
			if pagePath == "" {
				pagePath = "/server/" + page
			}
			request := httptest.NewRequest(http.MethodGet, pagePath, nil)
			response := httptest.NewRecorder()
			data := PageData{Data: map[string]interface{}{}}
			if page == "healthz" {
				data.Health = &healthPageView{
					HealthResponse: HealthResponse{
						Project: ProjectInfo{Name: "GitIgnore"}, Status: "healthy", Version: "test", GoVersion: "go1.test",
						Build: BuildInfo{Commit: "test"}, Mode: "production",
						Features: FeaturesInfo{Tor: TorInfo{Status: "disabled"}},
						Checks:   ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "ok", Datasets: map[string]string{}},
					},
					BannerClass: "status-ok", StatusIcon: "✓", StatusText: "Healthy", TimestampISO: "2026-01-01T00:00:00Z",
				}
			}
			s.renderPage(response, request, page, data)
			if response.Code != http.StatusOK {
				t.Fatalf("render status = %d", response.Code)
			}
			root, err := html.Parse(strings.NewReader(response.Body.String()))
			if err != nil {
				t.Fatal(err)
			}
			mainCount, headingCount, containerCount := 0, 0, 0
			containerWidthModifiers := make(map[string]int)
			breadcrumbCount := 0
			headerCount := 0
			var headerChildren []string
			var heading *html.Node
			var firstBodyAnchors []string
			var inspect func(*html.Node)
			inspect = func(node *html.Node) {
				if node.Type == html.ElementNode {
					if node.Data == "section" && hasClass(node, "section-card") && !sectionCardHasHeading(node) {
						t.Error(".section-card must be labelled by its own h2")
					}
					if node.Data == "section" && hasClass(node, "section-card") {
						for parent := node.Parent; parent != nil; parent = parent.Parent {
							if parent.Type == html.ElementNode && hasClass(parent, "section-card") {
								t.Error(".section-card must not be nested inside another card")
								break
							}
						}
					}
					switch node.Data {
					case "main":
						mainCount++
					case "h1":
						headingCount++
						heading = node
					case "div":
						if hasClass(node, "container") {
							containerCount++
							for _, class := range strings.Fields(attribute(node, "class")) {
								if class == "container-sm" || class == "container-md" || class == "container-lg" {
									containerWidthModifiers[class]++
								}
							}
						}
					case "nav":
						if hasClass(node, "breadcrumb") {
							breadcrumbCount++
						}
					case "header":
						if hasClass(node, "header") {
							headerCount++
							for child := node.FirstChild; child != nil; child = child.NextSibling {
								if child.Type == html.ElementNode {
									headerChildren = append(headerChildren, child.Data+"."+attribute(child, "class"))
								}
							}
						}
					case "form":
						if strings.EqualFold(attribute(node, "method"), "post") {
							if !containsDescendantInput(node, "csrf_token") {
								t.Errorf("POST form %q has no CSRF token", attribute(node, "action"))
							}
						}
					case "style":
						t.Error("inline style element is not allowed")
					case "script":
						if strings.TrimSpace(nodeText(node)) != "" {
							t.Error("inline script body is not allowed")
						}
					}
					for _, attr := range node.Attr {
						if attr.Key == "style" || strings.HasPrefix(strings.ToLower(attr.Key), "on") {
							t.Errorf("forbidden attribute %q on <%s>", attr.Key, node.Data)
						}
					}
					if node.Data == "img" && attribute(node, "alt") == "" {
						t.Error("image is missing alt text")
					}
					if node.Data == "input" || node.Data == "select" || node.Data == "textarea" {
						inputType := strings.ToLower(attribute(node, "type"))
						if node.Data != "input" || inputType != "hidden" && inputType != "submit" && inputType != "button" && inputType != "reset" && inputType != "image" {
							if !controlHasLabel(root, node) {
								t.Errorf("<%s> control %q has no visible label", node.Data, attribute(node, "name"))
							}
						}
					}
					if node.Data == "table" {
						if !hasDescendant(node, "thead") {
							t.Error("table is missing a thead")
						}
						if node.Parent == nil || !hasClass(node.Parent, "table-wrapper") {
							t.Error("table is not inside .table-wrapper")
						}
					}
					if node.Data == "a" && node.Parent != nil && node.Parent.Data == "body" && len(firstBodyAnchors) < 2 {
						firstBodyAnchors = append(firstBodyAnchors, attribute(node, "class"))
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					inspect(child)
				}
			}
			inspect(root)
			if mainCount != 1 || headingCount != 1 {
				t.Errorf("main count=%d h1 count=%d, want one of each", mainCount, headingCount)
			}
			if heading == nil || !hasClass(heading, "page-title") {
				t.Error("the single h1 does not use .page-title")
			}
			if containerCount != 1 {
				t.Errorf("container count=%d, want exactly one", containerCount)
			}
			widthModifierCount := 0
			for _, count := range containerWidthModifiers {
				widthModifierCount += count
			}
			if widthModifierCount != 1 {
				t.Errorf("container width modifiers = %v, want exactly one of container-sm, container-md, or container-lg", containerWidthModifiers)
			}
			if headerCount != 1 || strings.Join(headerChildren, ",") != "a.site-brand,nav.nav-links,label.nav-toggle,div.header-actions" {
				t.Errorf("header structure = %v (count=%d)", headerChildren, headerCount)
			}
			wantBreadcrumb := map[string]string{
				"mcp_help": "/server/help", "preferences_sync": "/server/preferences",
				"preferences_tokens": "/server/preferences", "security_policy": "/server/security",
				"security_thanks": "/server/security", "security_report_status": "/server/security",
			}[page]
			if wantBreadcrumb == "" {
				if breadcrumbCount != 0 {
					t.Errorf("hub or top-level page has %d breadcrumbs", breadcrumbCount)
				}
			} else if breadcrumbCount != 1 || !strings.Contains(response.Body.String(), `href="`+wantBreadcrumb+`"`) {
				t.Errorf("subpage must have exactly one breadcrumb to %s", wantBreadcrumb)
			}
			if len(firstBodyAnchors) != 2 || firstBodyAnchors[0] != "skip-link" || firstBodyAnchors[1] != "skip-link" {
				t.Errorf("first body links are not the two skip links: %v", firstBodyAnchors)
			}
		})
	}
}

func controlHasLabel(root, control *html.Node) bool {
	if id := attribute(control, "id"); id != "" {
		var found bool
		var visit func(*html.Node)
		visit = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "label" && attribute(node, "for") == id {
				found = true
			}
			for child := node.FirstChild; child != nil && !found; child = child.NextSibling {
				visit(child)
			}
		}
		visit(root)
		if found {
			return true
		}
	}
	for parent := control.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "label" {
			return true
		}
	}
	return false
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func hasDescendant(node *html.Node, name string) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == name || hasDescendant(child, name) {
			return true
		}
	}
	return false
}

func sectionCardHasHeading(card *html.Node) bool {
	labelID := attribute(card, "aria-labelledby")
	if labelID == "" {
		return false
	}
	var found bool
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "h2" && attribute(node, "id") == labelID {
			found = true
			return
		}
		for child := node.FirstChild; child != nil && !found; child = child.NextSibling {
			visit(child)
		}
	}
	visit(card)
	return found
}

func containsDescendantInput(node *html.Node, name string) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "input" && attribute(child, "name") == name || containsDescendantInput(child, name) {
			return true
		}
	}
	return false
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(nodeText(child))
	}
	return text.String()
}

func TestRegisteredHubPagesAreOrderedAndLocalized(t *testing.T) {
	s := newTestPageServer()
	preferences := s.registeredHubPages(true, "es")
	if len(preferences) != 2 {
		t.Fatalf("preferences page count = %d, want 2", len(preferences))
	}
	if preferences[0].Path != "/server/preferences/tokens" || preferences[1].Path != "/server/preferences/sync" {
		t.Fatalf("preferences pages are not in registry order: %#v", preferences)
	}
	if preferences[0].Title == "owner_tokens_page.title" || preferences[0].Description == "owner_tokens_page.description" {
		t.Fatalf("preferences page strings were not translated: %#v", preferences[0])
	}
	help := s.registeredHubPages(false, "en")
	if len(help) != 1 || help[0].Path != "/server/help/mcp" || help[0].Title != "MCP server" || help[0].Description == "" {
		t.Fatalf("unexpected help page registry: %#v", help)
	}
}

func TestHelpPageRegistryFeedsHubAndAPI(t *testing.T) {
	s := newTestPageServer()
	pageRequest := httptest.NewRequest(http.MethodGet, "/server/help", nil)
	pageResponse := httptest.NewRecorder()
	s.handleHelpPage(pageResponse, pageRequest)
	if pageResponse.Code != http.StatusOK || strings.Count(pageResponse.Body.String(), `href="/server/help/mcp"`) != 1 {
		t.Fatalf("help hub did not render the registered MCP page once: status=%d body=%s", pageResponse.Code, pageResponse.Body.String())
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/v1/server/help", nil)
	apiResponse := httptest.NewRecorder()
	s.handleAPIHelp(apiResponse, apiRequest)
	for _, expected := range []string{`"path":"/server/help/mcp"`, `"title":"MCP server"`, `"description":"Connect AI tools to the catalog with a linked owner token."`} {
		if !strings.Contains(apiResponse.Body.String(), expected) {
			t.Errorf("help API omitted registry value %s: %s", expected, apiResponse.Body.String())
		}
	}

	mcpRequest := httptest.NewRequest(http.MethodGet, "/server/help/mcp", nil)
	mcpResponse := httptest.NewRecorder()
	s.handleMCPHelpPage(mcpResponse, mcpRequest)
	if mcpResponse.Code != http.StatusOK || !strings.HasPrefix(mcpResponse.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("registered MCP page did not return HTML 200: status=%d content-type=%q", mcpResponse.Code, mcpResponse.Header().Get("Content-Type"))
	}
}

func TestRegisteredPreferencePagesRenderAsHTMLSubpages(t *testing.T) {
	s := newTestPageServer()
	handlers := map[string]http.HandlerFunc{
		"/server/preferences/tokens": s.handleOwnerTokensPage,
		"/server/preferences/sync":   s.handlePreferencesSyncPage,
	}
	for _, page := range s.registeredHubPages(true, "en") {
		handler := handlers[page.Path]
		if handler == nil {
			t.Fatalf("registered preference page %s has no route handler", page.Path)
		}
		response := doPage(handler, page.Path, "")
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
			t.Errorf("registered page %s returned status=%d content-type=%q", page.Path, response.Code, response.Header().Get("Content-Type"))
		}
		if strings.Count(response.Body.String(), `class="breadcrumb"`) != 1 || !strings.Contains(response.Body.String(), `href="/server/preferences"`) {
			t.Errorf("registered subpage %s does not render one parent-hub breadcrumb", page.Path)
		}
	}
}

// TestThemeClassDefaultsDark verifies the server renders theme-dark with no
// theme cookie present (AI.md PART 16 "Theme preference source", default dark).
func TestThemeClassDefaultsDark(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleAboutPage, "/server/about", "")
	if !strings.Contains(rec.Body.String(), `class="theme-dark"`) {
		t.Errorf("expected theme-dark default on <html>, got:\n%s", rec.Body.String()[:200])
	}
}

// TestThemeClassFromCookie verifies a valid theme cookie is rendered on <html>.
func TestThemeClassFromCookie(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleAboutPage, "/server/about", "light")
	if !strings.Contains(rec.Body.String(), `class="theme-light"`) {
		t.Errorf("expected theme-light from cookie, body head:\n%s", rec.Body.String()[:200])
	}
}

// TestThemeClassInvalidCookieFallsBack verifies an invalid theme cookie falls
// back to the dark default rather than emitting an arbitrary class.
func TestThemeClassInvalidCookieFallsBack(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleAboutPage, "/server/about", "neon")
	if !strings.Contains(rec.Body.String(), `class="theme-dark"`) {
		t.Errorf("expected fallback to theme-dark for invalid cookie")
	}
}

// TestThemeSetPersistsCookie verifies the no-JS theme endpoint sets the cookie
// and redirects (AI.md PART 16 <noscript> fallback).
func TestThemeSetPersistsCookie(t *testing.T) {
	s := newTestPageServer()
	csrfToken := strings.Repeat("a", 64)
	req := httptest.NewRequest(http.MethodPost, "/server/theme", strings.NewReader("theme=light&return=/&csrf_token="+csrfToken))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	rec := httptest.NewRecorder()
	s.handleThemeSet(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "theme" && c.Value == "light" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected theme=light cookie to be set")
	}
}

func TestEnsureCSRFTokenReusesCookie(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	first := httptest.NewRecorder()
	token := ensureCSRFToken(first, request)
	if len(token) != 64 {
		t.Fatalf("CSRF token length = %d, want 64", len(token))
	}
	issued := first.Result().Cookies()
	if len(issued) != 1 || issued[0].Name != "csrf_token" || issued[0].Value != token || !issued[0].HttpOnly {
		t.Fatalf("issued CSRF cookie = %#v", issued)
	}
	request.AddCookie(issued[0])
	second := httptest.NewRecorder()
	if got := ensureCSRFToken(second, request); got != token {
		t.Fatalf("reused token = %q, want original", got)
	}
	if len(second.Result().Cookies()) != 0 {
		t.Fatal("existing valid CSRF cookie was reissued")
	}
}

func TestValidCSRFRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: strings.Repeat("a", 64)})
	request.Form = url.Values{"csrf_token": {strings.Repeat("a", 64)}}
	if !validCSRFRequest(request) {
		t.Fatal("matching CSRF token was rejected")
	}
	request.Form.Set("csrf_token", strings.Repeat("b", 64))
	if validCSRFRequest(request) {
		t.Fatal("mismatched CSRF token was accepted")
	}
	request.Form.Set("csrf_token", "short")
	if validCSRFRequest(request) {
		t.Fatal("short CSRF token was accepted")
	}
}

// TestThemeSetRejectsOpenRedirect verifies an external return target is ignored.
func TestThemeSetRejectsOpenRedirect(t *testing.T) {
	s := newTestPageServer()
	csrfToken := strings.Repeat("a", 64)
	req := httptest.NewRequest(http.MethodPost, "/server/theme", strings.NewReader("theme=dark&return=//evil.example&csrf_token="+csrfToken))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	rec := httptest.NewRecorder()
	s.handleThemeSet(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}
}

// TestNotFoundRendersThemedPage verifies the 404 handler returns a themed HTML
// error page (AI.md PART 16 "Error Pages MUST Match Theme").
func TestNotFoundRendersThemedPage(t *testing.T) {
	s := newTestPageServer()
	rec := doPage(s.handleNotFound, "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "theme-dark") {
		t.Errorf("404 page not themed")
	}
	if !strings.Contains(body, "404") {
		t.Errorf("404 page missing status code")
	}
}

// TestManifestIconsExist verifies the manifest references an icon that is
// actually embedded, closing the broken-icon gap from the audit.
func TestManifestIconsExist(t *testing.T) {
	s := newTestPageServer()
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rec := httptest.NewRecorder()
	s.handleManifest(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "/static/images/icon-192.png") {
		t.Errorf("manifest still references non-existent PNG icon")
	}
	if !strings.Contains(body, "/static/images/icon.svg") {
		t.Fatalf("manifest missing icon.svg reference")
	}
	if _, err := staticHTTPFS.Open("images/icon.svg"); err != nil {
		t.Errorf("referenced icon.svg is not embedded: %v", err)
	}
}
