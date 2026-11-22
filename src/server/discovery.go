package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleLLMSTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	var body bytes.Buffer
	name := "GitIgnore API"
	description := "A curated catalog of .gitignore templates with a versioned REST API and server-rendered browser."
	apiContact, securityContact := "", ""
	rateLimit := "disabled"
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
			rateLimit = fmt.Sprintf("%d requests/minute", requests)
		}
	}
	baseURL := s.publicServerURL(r)
	apiURL := baseURL + s.apiBasePath()
	_, _ = fmt.Fprintf(&body, "# %s\n> %s\n\n", name, description)
	_, _ = fmt.Fprintf(&body, "## API\nBase URL: %s\nAuthentication: none for the REST API; a Bearer token links what you create to you and is REQUIRED for MCP (get one from POST %s/server/preferences/tokens/primary or the New token button on %s/server/preferences/tokens, then create one linked token per MCP client there)\nRate limit: %s\nMCP: %s/mcp (Streamable HTTP; Bearer token required)\n\n", apiURL, apiURL, baseURL, rateLimit, baseURL)
	if cfg := s.currentConfig(); cfg == nil || cfg.Web.LLMS.IncludeEndpoints {
		_, _ = fmt.Fprintln(&body, "## Endpoints")
		for _, endpoint := range s.llmsEndpoints() {
			_, _ = fmt.Fprintf(&body, "- %s\n", endpoint)
		}
		_, _ = fmt.Fprintln(&body)
	}
	_, _ = fmt.Fprintln(&body, "## Capabilities\n- Browse and search curated .gitignore templates\n- Retrieve templates as text or JSON\n- Combine multiple templates\n- Use the versioned REST API, GraphQL, or MCP")
	if cfg := s.currentConfig(); cfg != nil {
		if cfg.Web.LLMS.IncludeSchemas {
			if schema, err := json.MarshalIndent(s.openAPISpec(r), "", "  "); err == nil {
				_, _ = fmt.Fprintf(&body, "\n## OpenAPI request and response schemas\n```json\n%s\n```\n", schema)
			}
		}
		for _, section := range cfg.Web.LLMS.CustomSections {
			section = strings.TrimSpace(section)
			if section != "" {
				_, _ = fmt.Fprintf(&body, "\n%s\n", section)
			}
		}
	}
	_, _ = fmt.Fprintf(&body, "\n## Contact\nAPI issues: %s\nSecurity: %s\n", emptyLabel(apiContact), emptyLabel(securityContact))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func (s *Server) llmsEndpoints() []string {
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
			auth = " (Bearer token required)"
		}
		endpoints = append(endpoints, fmt.Sprintf("%s %s%s", method, route, auth))
		return nil
	})
	sort.Strings(endpoints)
	return endpoints
}

func emptyLabel(value string) string {
	if value == "" {
		return "not configured"
	}
	return value
}
