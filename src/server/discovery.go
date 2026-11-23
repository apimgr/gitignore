package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleLLMSTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	var body bytes.Buffer
	name := "GitIgnore API"
	description := "A curated catalog of .gitignore templates with a versioned REST API and server-rendered browser."
	apiContact, securityContact := "", ""
	rateLimit := i18n.T(r, "llms.rate_limit_disabled")
	if cfg := s.currentConfig(); cfg != nil {
		if value := strings.TrimSpace(cfg.Server.Branding.Title); value != "" {
			name = value
		}
		if value := strings.TrimSpace(cfg.Server.Branding.Description); value != "" {
			description = value
		}
		apiContact = strings.TrimSpace(cfg.Web.APIContact)
		securityContact = strings.TrimSpace(cfg.Web.Security.Contact)
		if cfg.Server.RateLimit.Enabled {
			window := cfg.Server.RateLimit.Window
			if window < 1 {
				window = 60
			}
			requests := cfg.Server.RateLimit.Requests * 60 / window
			rateLimit = i18n.TF(r, "llms.requests_per_minute", "count", fmt.Sprint(requests))
		}
	}
	baseURL := s.publicServerURL(r)
	apiURL := baseURL + s.apiBasePath()
	_, _ = fmt.Fprintf(&body, "# %s\n> %s\n\n", name, description)
	_, _ = fmt.Fprintf(&body, "## %s\n%s\n%s\n%s\n%s\n\n",
		i18n.T(r, "llms.api"),
		i18n.TF(r, "llms.base_url", "url", apiURL),
		i18n.TF(r, "llms.authentication", "primary_url", "POST "+apiURL+"/server/preferences/tokens/primary", "tokens_url", baseURL+"/server/preferences/tokens"),
		i18n.TF(r, "llms.rate_limit", "value", rateLimit),
		i18n.TF(r, "llms.mcp", "url", baseURL))
	if cfg := s.currentConfig(); cfg == nil || cfg.Web.LLMS.IncludeEndpoints {
		_, _ = fmt.Fprintf(&body, "## %s\n", i18n.T(r, "llms.endpoints"))
		for _, endpoint := range s.llmsEndpoints(i18n.LangFromContext(r.Context())) {
			_, _ = fmt.Fprintf(&body, "- %s\n", endpoint)
		}
		_, _ = fmt.Fprintln(&body)
	}
	_, _ = fmt.Fprintf(&body, "## %s\n- %s\n- %s\n- %s\n- %s\n",
		i18n.T(r, "llms.capabilities"), i18n.T(r, "llms.browse"), i18n.T(r, "llms.retrieve"),
		i18n.T(r, "llms.combine"), i18n.T(r, "llms.protocols"))
	if cfg := s.currentConfig(); cfg != nil {
		if cfg.Web.LLMS.IncludeSchemas {
			if schema, err := json.MarshalIndent(s.openAPISpec(r), "", "  "); err == nil {
				_, _ = fmt.Fprintf(&body, "\n## %s\n```json\n%s\n```\n", i18n.T(r, "llms.schemas"), schema)
			}
		}
		for _, section := range cfg.Web.LLMS.CustomSections {
			section = strings.TrimSpace(section)
			if section != "" {
				_, _ = fmt.Fprintf(&body, "\n%s\n", section)
			}
		}
	}
	_, _ = fmt.Fprintf(&body, "\n## %s\n%s: %s\n%s: %s\n", i18n.T(r, "llms.contact"),
		i18n.T(r, "llms.api_issues"), emptyLabel(r, apiContact), i18n.T(r, "llms.security"), emptyLabel(r, securityContact))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func (s *Server) llmsEndpoints(lang string) []string {
	endpoints := make([]string, 0, 64)
	_ = chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, s.apiBasePath()) {
			return nil
		}
		if strings.Contains(route, "/server/metrics") || strings.Contains(route, "/server/debug") || strings.Contains(route, "/server/maintenance") {
			return nil
		}
		auth := ""
		if strings.Contains(route, "/preferences/tokens") || strings.HasSuffix(route, "/server/mcp") {
			auth = i18n.Translate(lang, "llms.bearer_required")
		}
		endpoints = append(endpoints, fmt.Sprintf("%s %s%s", method, route, auth))
		return nil
	})
	sort.Strings(endpoints)
	return endpoints
}

func emptyLabel(r *http.Request, value string) string {
	if value == "" {
		return i18n.T(r, "llms.not_configured")
	}
	return value
}
