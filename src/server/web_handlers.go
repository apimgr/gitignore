package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/db"
	"github.com/go-chi/chi/v5"
)

// Web page handlers render server-side HTML templates (AI.md PART 16).

// handleSearchPage serves the search page.
func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	data := map[string]interface{}{"query": query}
	if query != "" {
		data["results"] = s.config.Templates.Search(query)
	}
	s.renderPage(w, r, "search", PageData{Title: "Search", Data: data})
}

// handleTemplatePage serves the template detail page.
func (s *Server) handleTemplatePage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	tmpl, err := s.config.Templates.Get(name)
	if err != nil {
		s.renderPageStatus(w, r, "template", http.StatusNotFound, PageData{
			Title: "Not found",
			Data:  map[string]interface{}{"name": name, "content": "template not found"},
		})
		return
	}
	s.renderPage(w, r, "template", PageData{
		Title: tmpl.Name,
		Data:  map[string]interface{}{"name": tmpl.Name, "content": tmpl.Content},
	})
}

// handleCombinePage serves the combine templates page.
func (s *Server) handleCombinePage(w http.ResponseWriter, r *http.Request) {
	param := r.URL.Query().Get("templates")
	data := map[string]interface{}{"templates": param}
	if param != "" {
		names := strings.Split(param, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		combined, err := s.config.Templates.Combine(names)
		if err != nil {
			data["error"] = err.Error()
		} else {
			data["content"] = combined
		}
	}
	s.renderPage(w, r, "combine", PageData{Title: "Combine", Data: data})
}

// handleCategoriesPage serves the categories page.
func (s *Server) handleCategoriesPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "categories", PageData{
		Title: "Categories",
		Data:  map[string]interface{}{"categories": s.config.Templates.GetCategories()},
	})
}

// handleListPage serves the list-all-templates page.
func (s *Server) handleListPage(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	var names []string
	if category != "" {
		for _, t := range s.config.Templates.GetByCategory(category) {
			names = append(names, t.Name)
		}
	} else {
		names = s.config.Templates.List()
	}
	s.renderPage(w, r, "list", PageData{
		Title: "All Templates",
		Data:  map[string]interface{}{"templates": names},
	})
}

// handleStatsPage serves the statistics page.
func (s *Server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "stats", PageData{Title: "Statistics", Data: s.config.Templates.Stats()})
}

// handleDocsPage serves the API documentation page.
func (s *Server) handleDocsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "docs", PageData{Title: "API Documentation"})
}

// handleCLIPage serves the CLI customization page.
func (s *Server) handleCLIPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "cli", PageData{Title: "CLI"})
}

// handleServerPage serves the /server index page.
func (s *Server) handleServerPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "server", PageData{Title: "Server"})
}

// handleAboutPage serves the About standard page (AI.md PART 16).
func (s *Server) handleAboutPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "about", PageData{Title: "About"})
}

func (s *Server) handleAPIAbout(w http.ResponseWriter, r *http.Request) {
	sendAPIResponseOK(w, map[string]string{
		"title":   "About GitIgnore",
		"content": "GitIgnore is a curated .gitignore template service with a public REST API, read-only GraphQL API, MCP tools, server-rendered browser interface, and companion CLI. Templates are embedded in the binary and can be retrieved individually, searched, or combined.",
		"format":  "markdown",
	})
}

func (s *Server) handleDPOPage(w http.ResponseWriter, r *http.Request) {
	name, email := "", ""
	if s.currentConfig() != nil {
		name = s.currentConfig().Server.Contact.DPO.Name
		email = s.currentConfig().Server.Contact.DPO.Email
		if email == "" {
			email = s.currentConfig().Server.Contact.General.Email
		}
	}
	s.renderPage(w, r, "dpo", PageData{Title: "Data Protection Officer", Data: map[string]interface{}{"name": name, "email": email}})
}

func (s *Server) handleAPIDPO(w http.ResponseWriter, r *http.Request) {
	name, email := "", ""
	if s.currentConfig() != nil {
		name = s.currentConfig().Server.Contact.DPO.Name
		email = s.currentConfig().Server.Contact.DPO.Email
		if email == "" {
			email = s.currentConfig().Server.Contact.General.Email
		}
	}
	sendAPIResponseOK(w, map[string]string{"name": name, "email": email, "privacy_url": s.publicServerURL(r) + "/server/privacy"})
}

// handlePrivacyPage serves the Privacy standard page.
func (s *Server) handlePrivacyPage(w http.ResponseWriter, r *http.Request) {
	active := s.currentConfig()
	if active == nil {
		active = config.DefaultConfig()
	}
	cfg := active.Server
	analytics := cfg.Privacy.Cookies.Analytics.Description
	if cfg.Tracking.Type != "" {
		if cfg.Privacy.Data.Sold {
			analytics += " " + cfg.Privacy.Cookies.Analytics.DescriptionSuffixSold
		} else {
			analytics += " " + cfg.Privacy.Cookies.Analytics.DescriptionSuffixNotSold
		}
	}
	stored := cfg.Privacy.Data.StoredOnServer == nil || *cfg.Privacy.Data.StoredOnServer
	contact := strings.TrimSpace(cfg.Contact.DPO.Email)
	if contact == "" {
		contact = strings.TrimSpace(cfg.Contact.General.Email)
	}
	ccpaOptedOut := false
	if cookie, err := r.Cookie("ccpa_opt_out"); err == nil && cookie.Value == "true" {
		ccpaOptedOut = true
	}
	data := map[string]interface{}{
		"privacy": cfg.Privacy, "storedOnServer": stored, "analyticsDescription": analytics,
		"trackingType": cfg.Tracking.Type, "trackingName": trackingProviderName(cfg.Tracking.Type),
		"ccpaOptedOut": ccpaOptedOut, "contactEmail": contact,
		"extension": s.operatorHTML(r, "privacy"), "dataCollection": renderPublicContent(privacyCollection, "markdown"),
		"dataUsage": renderPublicContent(privacyDataUsage(cfg.Privacy.Data.Sold), "markdown"), "dataSecurity": renderPublicContent(privacySecurity, "markdown"),
	}
	s.renderPage(w, r, "privacy", PageData{Title: "Privacy", Data: data})
}

func (s *Server) handleAPIPrivacy(w http.ResponseWriter, r *http.Request) {
	active := s.currentConfig()
	if active == nil {
		active = config.DefaultConfig()
	}
	cfg := active.Server
	trackingName := trackingProviderName(cfg.Tracking.Type)
	analytics := cfg.Privacy.Cookies.Analytics.Description
	if cfg.Tracking.Type != "" {
		if cfg.Privacy.Data.Sold {
			analytics += " " + cfg.Privacy.Cookies.Analytics.DescriptionSuffixSold
		} else {
			analytics += " " + cfg.Privacy.Cookies.Analytics.DescriptionSuffixNotSold
		}
	}
	stored := cfg.Privacy.Data.StoredOnServer == nil || *cfg.Privacy.Data.StoredOnServer
	optedOut := false
	if cookie, err := r.Cookie("ccpa_opt_out"); err == nil && cookie.Value == "true" {
		optedOut = true
	}
	sendAPIResponseOK(w, map[string]interface{}{
		"summary":   map[string]bool{"data_stored_on_server": stored, "data_sold": cfg.Privacy.Data.Sold, "user_control": true},
		"cookies":   map[string]interface{}{"essential": cfg.Privacy.Cookies.Essential, "preferences": cfg.Privacy.Cookies.Preferences, "analytics": map[string]interface{}{"enabled": cfg.Privacy.Cookies.Analytics.Enabled, "description": analytics}},
		"data":      map[string]interface{}{"sold": cfg.Privacy.Data.Sold, "stored_on_server": stored, "sharing": cfg.Privacy.Data.Sharing},
		"tracking":  map[string]interface{}{"enabled": cfg.Tracking.Type != "", "type": cfg.Tracking.Type, "type_name": trackingName},
		"retention": cfg.Privacy.Retention, "third_party": cfg.Privacy.ThirdParty,
		"ccpa":    map[string]interface{}{"applicable": cfg.Privacy.Data.Sold, "opt_out_url": "/server/privacy#ccpa-opt-out", "user_opted_out": optedOut},
		"content": map[string]string{"consent_message": privacyConsentMessage(cfg.Privacy), "data_usage": privacyDataUsage(cfg.Privacy.Data.Sold)},
	})
}

// handleContactPage serves the Contact standard page.
func (s *Server) handleContactPage(w http.ResponseWriter, r *http.Request) {
	if !s.contactPageEnabled() {
		s.handleNotFound(w, r)
		return
	}
	s.renderContact(w, r, http.StatusOK, nil)
}

// handleHelpPage serves the Help standard page.
func (s *Server) handleHelpPage(w http.ResponseWriter, r *http.Request) {
	content := ""
	if s.config != nil && s.currentConfig() != nil {
		content = s.currentConfig().Server.Pages.Help.Content
	}
	lang := i18n.LangFromContext(r.Context())
	s.renderPage(w, r, "help", PageData{Title: "Help", Data: map[string]interface{}{"project": content, "registeredPages": s.registeredHubPages(false, lang)}})
}

func (s *Server) handleAPIHelp(w http.ResponseWriter, r *http.Request) {
	content := ""
	if s.config != nil && s.currentConfig() != nil {
		content = s.currentConfig().Server.Pages.Help.Content
	}
	sendAPIResponseOK(w, map[string]interface{}{
		"title":   "Help",
		"content": content,
		"format":  "markdown",
		"pages":   s.registeredHubPages(false, i18n.LangFromContext(r.Context())),
	})
}

func (s *Server) handleMCPHelpPage(w http.ResponseWriter, r *http.Request) {
	serverURL := s.publicServerURL(r)
	tools := s.mcpTools()
	project := s.mcpProjectDescription()
	data := map[string]interface{}{
		"url":     serverURL + s.apiBasePath() + "/server/mcp",
		"apiURL":  serverURL + "/api/mcp",
		"rootURL": serverURL + "/mcp",
		"tools":   tools,
		"project": project,
	}
	s.renderPage(w, r, "mcp_help", PageData{Title: "MCP help", Data: data})
}

func (s *Server) handleAPIHelpMCP(w http.ResponseWriter, r *http.Request) {
	serverURL := s.publicServerURL(r)
	tools := s.mcpTools()
	project := s.mcpProjectDescription()
	descriptions := make([]map[string]string, 0, len(tools))
	for _, tool := range tools {
		descriptions = append(descriptions, map[string]string{"name": tool.Name, "description": tool.Description})
	}
	sendAPIResponseOK(w, map[string]interface{}{
		"enabled": true,
		"url":     serverURL + s.apiBasePath() + "/server/mcp",
		"aliases": []string{serverURL + "/api/mcp", serverURL + "/mcp"},
		"auth":    map[string]string{"scheme": "Bearer"},
		"tools":   descriptions,
		"project": project,
	})
}

func (s *Server) mcpProjectDescription() string {
	if s.config != nil && s.currentConfig() != nil {
		return s.currentConfig().Server.Pages.Help.MCP
	}
	return "Use the public catalog tools to list, search, retrieve, combine, and export curated .gitignore templates. Template names are case-insensitive."
}

// handleTermsPage serves the Terms standard page.
func (s *Server) handleTermsPage(w http.ResponseWriter, r *http.Request) {
	content, format := s.termsContent(r)
	entry := s.operatorHTML(r, "terms")
	if entry != "" {
		content = string(entry)
		format = "html"
	}
	s.renderPage(w, r, "terms", PageData{Title: "Terms of Service", Data: map[string]interface{}{"content": renderPublicContent(content, format)}})
}

func (s *Server) handleAPITerms(w http.ResponseWriter, r *http.Request) {
	content, format := s.termsContent(r)
	sendAPIResponseOK(w, map[string]string{"title": "Terms of Service", "content": content, "format": format})
}

// handleSecurityPage serves the public security reporting guidance.
func (s *Server) handleSecurityPage(w http.ResponseWriter, r *http.Request) {
	id, err := securityID(time.Now())
	serverURL := s.publicServerURL(r)
	reportURL := ""
	if err == nil {
		reportURL = securityReportURL(serverURL, id)
	}
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.ReportURL) != "" {
		reportURL = strings.TrimSpace(s.currentConfig().Web.Security.ReportURL)
	}
	expires := time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339)
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Expires) != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(s.currentConfig().Web.Security.Expires)); parseErr == nil {
			expires = parsed.UTC().Format(time.RFC3339)
		}
	}
	pgpAvailable := false
	pgpFingerprint := ""
	if metadata, ok := s.visiblePGPMetadata(); ok {
		pgpAvailable = true
		pgpFingerprint = metadata.Fingerprint
	}
	s.renderPage(w, r, "security", PageData{
		Title: "Security",
		Data: map[string]interface{}{
			"email":          s.securityContactEmail(),
			"expires":        expires,
			"reportURL":      reportURL,
			"pgpAvailable":   pgpAvailable,
			"pgpFingerprint": pgpFingerprint,
		},
	})
}

// handleAPISecurity returns the request-specific security reporting details.
func (s *Server) handleAPISecurity(w http.ResponseWriter, r *http.Request) {
	id, err := securityID(time.Now())
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "security reporting is unavailable")
		return
	}
	serverURL := s.publicServerURL(r)
	githubReportURL := "https://github.com/apimgr/gitignore/security/advisories/new"
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.ReportURL) != "" {
		githubReportURL = strings.TrimSpace(s.currentConfig().Web.Security.ReportURL)
	}
	contacts := []map[string]string{{"type": "github", "value": githubReportURL}}
	if s.contactPageEnabled() {
		contacts = append(contacts, map[string]string{"type": "contact_form", "value": securityReportURL(serverURL, id)})
	}
	if email := s.securityContactEmail(); email != "" {
		contacts = append(contacts, map[string]string{"type": "email", "value": "mailto:" + email})
	}
	pgpData := map[string]interface{}{"available": false, "url": nil, "fingerprint": nil}
	if metadata, ok := s.visiblePGPMetadata(); ok {
		pgpData = map[string]interface{}{"available": true, "url": serverURL + "/.well-known/pgp-key.asc", "fingerprint": metadata.Fingerprint}
	}
	expires := time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339)
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Expires) != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(s.currentConfig().Web.Security.Expires)); parseErr == nil {
			expires = parsed.UTC().Format(time.RFC3339)
		}
	}
	data := map[string]interface{}{
		"contacts":   contacts,
		"expires":    expires,
		"pgp":        pgpData,
		"policy_url": serverURL + "/server/security/policy",
		"thanks_url": serverURL + "/server/security/thanks",
	}
	if s.contactPageEnabled() {
		data["report_url"] = serverURL + "/server/contact"
	}
	sendAPIResponseOK(w, data)
}

// handleSecurityPolicyPage serves the repository's disclosure policy.
func (s *Server) handleSecurityPolicyPage(w http.ResponseWriter, r *http.Request) {
	policy := securityPolicyMarkdown
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Policy) != "" {
		policy = s.currentConfig().Web.Security.Policy
	}
	s.renderPage(w, r, "security_policy", PageData{
		Title: "Security Policy",
		Data:  map[string]interface{}{"email": s.securityContactEmail(), "policy": policy},
	})
}

func (s *Server) handleAPISecurityPolicy(w http.ResponseWriter, r *http.Request) {
	policy := securityPolicyMarkdown
	if s.currentConfig() != nil && strings.TrimSpace(s.currentConfig().Web.Security.Policy) != "" {
		policy = s.currentConfig().Web.Security.Policy
	}
	sendAPIResponseOK(w, map[string]string{"policy": policy})
}

// handleSecurityThanksPage serves the public researcher acknowledgments page.
func (s *Server) handleSecurityThanksPage(w http.ResponseWriter, r *http.Request) {
	entries, err := db.ListSecurityThanks()
	if err != nil {
		s.renderPageStatus(w, r, "security_thanks", http.StatusInternalServerError, PageData{Title: "Security Acknowledgments"})
		return
	}
	s.renderPage(w, r, "security_thanks", PageData{Title: "Security Acknowledgments", Data: map[string]interface{}{"entries": entries}})
}

func (s *Server) handleAPISecurityThanks(w http.ResponseWriter, r *http.Request) {
	entries, err := db.ListSecurityThanks()
	if err != nil {
		sendAPIResponseError(w, "SERVER_ERROR", "security acknowledgments are unavailable")
		return
	}
	sendAPIResponseOK(w, map[string]interface{}{"entries": entries})
}

const securityPolicyMarkdown = "## Reporting a vulnerability\n\nReport security vulnerabilities privately through GitHub private vulnerability reporting or the security report form. Include the affected version, impact, and reproduction steps. Do not publish vulnerability details before coordinating disclosure.\n\n## Response process\n\nReceipt is acknowledged within 48 hours. Critical vulnerabilities are targeted for a fix within 14 days. The disclosure timeline is coordinated after a fix is available. Only the latest release receives security updates."

// handleSecurityReportStatus consumes the report's researcher status token at
// most once per UTC day and returns only triage metadata.
func (s *Server) handleSecurityReportStatus(w http.ResponseWriter, r *http.Request) {
	trackingID := chi.URLParam(r, "trackingID")
	token := r.URL.Query().Get("token")
	if !strings.HasPrefix(trackingID, "sec_") || len(token) != 32 {
		s.handleNotFound(w, r)
		return
	}
	tokenHash := sha256.Sum256([]byte(token))
	status, comment, disclosureDate, err := db.ClaimSecurityReportStatus(trackingID, hex.EncodeToString(tokenHash[:]), time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	s.renderPage(w, r, "security_report_status", PageData{
		Title: "Security Report Status",
		Data:  map[string]interface{}{"trackingID": trackingID, "status": status, "comment": comment, "disclosureDate": disclosureDate},
	})
}

// handleThemeSet is the no-JS theme switch: it persists the chosen theme in the
// "theme" cookie and redirects back, so a visitor without JavaScript can still
// change theme via the <noscript> form (AI.md PART 16).
func (s *Server) handleThemeSet(w http.ResponseWriter, r *http.Request) {
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	theme := r.FormValue("theme")
	if !validThemes[theme] {
		theme = "dark"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "theme",
		Value:    theme,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
		Secure:   preferenceCookieSecure(s, r),
	})
	dest := safePreferenceRedirect(s, r, "/")
	redirectPreference(w, dest)
}

func (s *Server) handlePreferencesPage(w http.ResponseWriter, r *http.Request) {
	consent := map[string]interface{}{}
	if cookie, err := r.Cookie("cookie_consent"); err == nil {
		var decoded map[string]interface{}
		if json.Unmarshal([]byte(cookie.Value), &decoded) == nil {
			consent = decoded
		}
	}
	ccpaSet := false
	if cookie, err := r.Cookie("ccpa_opt_out"); err == nil && cookie.Value == "true" {
		ccpaSet = true
	}
	s.renderPage(w, r, "preferences", PageData{Title: "Preferences", Data: map[string]interface{}{
		"cancelURL":          safePreferenceRedirect(s, r, "/"),
		"consentPreferences": consent["preferences"] == true,
		"consentAnalytics":   consent["analytics"] == true,
		"ccpaOptOut":         ccpaSet,
		"showCCPA":           s.config != nil && s.currentConfig() != nil && s.currentConfig().Server.Privacy.Data.Sold,
		"registeredPages":    s.registeredHubPages(true, i18n.LangFromContext(r.Context())),
	}})
}

func (s *Server) handlePreferencesSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !validCSRFRequest(r) {
		sendAPIResponseError(w, "CSRF_FAILED", "invalid request token")
		return
	}
	if err := r.ParseForm(); err != nil {
		r.Form = url.Values{}
	}
	if r.FormValue("action") == "reset" {
		s.applyPreferences(w, r, nil, true)
		redirectPreference(w, safePreferenceRedirect(s, r, "/"))
		return
	}
	values := preferenceInput(r, true, true)
	s.applyPreferences(w, r, values, false)
	if r.FormValue("consent_form") == "1" {
		consent, _ := json.Marshal(map[string]interface{}{"essential": true, "preferences": r.FormValue("consent_preferences") == "on", "analytics": r.FormValue("consent_analytics") == "on", "timestamp": time.Now().Unix()})
		setPreferenceCookie(w, r, "cookie_consent", string(consent), s)
		if s.config != nil && s.currentConfig() != nil && s.currentConfig().Server.Privacy.Data.Sold {
			if r.FormValue("ccpa_opt_out") == "on" {
				setPreferenceCookie(w, r, "ccpa_opt_out", "true", s)
			} else {
				expirePreferenceCookie(w, r, "ccpa_opt_out", s)
			}
		}
	}
	if r.FormValue("lang") == "auto" {
		expirePreferenceCookie(w, r, "lang", s)
	}
	redirectPreference(w, safePreferenceRedirect(s, r, "/server/preferences"))
}

func validCSRFRequest(r *http.Request) bool {
	cookie, err := r.Cookie("csrf_token")
	if err != nil || len(cookie.Value) != 64 {
		return false
	}
	formToken := r.FormValue("csrf_token")
	return len(formToken) == len(cookie.Value) && subtle.ConstantTimeCompare([]byte(formToken), []byte(cookie.Value)) == 1
}

func (s *Server) handleCCPA(w http.ResponseWriter, r *http.Request) {
	if !validCSRFRequest(r) {
		http.Error(w, "invalid request token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if s.currentConfig() == nil || !s.currentConfig().Server.Privacy.Data.Sold {
		http.NotFound(w, r)
		return
	}
	switch r.FormValue("choice") {
	case "opt-out":
		setPreferenceCookie(w, r, "ccpa_opt_out", "true", s)
	case "opt-in":
		expirePreferenceCookie(w, r, "ccpa_opt_out", s)
	default:
		http.Error(w, "invalid choice", http.StatusBadRequest)
		return
	}
	redirect := safePreferenceRedirect(s, r, "/server/privacy")
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// renderErrorPage renders a themed error page (AI.md PART 16 "Error Pages").
// It never leaks stack traces; the message is a short, user-facing string.
func (s *Server) renderErrorPage(w http.ResponseWriter, r *http.Request, code int, message string) {
	s.renderPageStatus(w, r, "error", code, PageData{
		Title: http.StatusText(code),
		Data: map[string]interface{}{
			"code":    code,
			"status":  http.StatusText(code),
			"message": message,
		},
	})
}

// handleNotFound renders the themed 404 page for unmatched routes.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderErrorPage(w, r, http.StatusNotFound, "The page you requested could not be found.")
}

// handleMethodNotAllowed renders the themed 405 page.
func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.renderErrorPage(w, r, http.StatusMethodNotAllowed, "That method is not allowed on this resource.")
}

// handleGraphiQLPage serves the GraphiQL playground.
func (s *Server) handleGraphiQLPage(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFromContext(r.Context())
	s.renderPage(w, r, "graphql_ui", PageData{
		Title:       i18n.Translate(lang, "help.graphql_description"),
		PageCSS:     []string{s.requestPath(r, "/static/vendor/graphiql/graphiql.min.css"), s.requestPath(r, "/static/vendor/graphiql/graphiql-custom.css")},
		PageScripts: []string{s.requestPath(r, "/static/vendor/graphiql/react.production.min.js"), s.requestPath(r, "/static/vendor/graphiql/react-dom.production.min.js"), s.requestPath(r, "/static/vendor/graphiql/graphiql.min.js"), s.requestPath(r, "/static/vendor/graphiql/graphiql-init.js")},
		Data:        map[string]interface{}{"endpoint": s.requestPath(r, s.apiBasePath()+"/server/graphql")},
	})
}
