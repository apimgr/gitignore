package server

import (
	"encoding/json"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/audit"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
)

type consentState struct {
	Essential   bool  `json:"essential"`
	Preferences bool  `json:"preferences"`
	Analytics   bool  `json:"analytics"`
	Timestamp   int64 `json:"timestamp"`
}

type trackingProvider struct {
	tags  template.HTML
	body  string
	hosts []string
}

func readConsent(r *http.Request) (consentState, bool) {
	cookie, err := r.Cookie("cookie_consent")
	if err != nil {
		return consentState{}, false
	}
	value, err := url.PathUnescape(cookie.Value)
	if err != nil {
		return consentState{}, false
	}
	var consent consentState
	if json.Unmarshal([]byte(value), &consent) != nil || !consent.Essential || consent.Timestamp <= 0 {
		return consentState{}, false
	}
	return consent, true
}

func encodeConsentCookie(value []byte) string {
	return url.PathEscape(string(value))
}

func (s *Server) trackingAllowed(r *http.Request) bool {
	if s == nil || r == nil || s.currentConfig() == nil || s.isTorRequest(r) || isI2PHost(r.Host) {
		return false
	}
	consent, ok := readConsent(r)
	provider := trackingProviderFor(s.currentConfig().Server.Tracking)
	return ok && consent.Analytics && (provider.body != "" || len(provider.tags) != 0)
}

func (s *Server) consentView(r *http.Request, csrf string) ConsentView {
	view := ConsentView{CSRFToken: csrf}
	if s == nil || r == nil || s.currentConfig() == nil || s.isTorRequest(r) || isI2PHost(r.Host) {
		return view
	}
	if _, valid := readConsent(r); valid {
		return view
	}
	privacy := localizedPrivacyDefaults(r, s.currentConfig().Server.Privacy)
	view.Show = true
	view.DataSold = privacy.Data.Sold
	view.Message = privacy.Consent.Message
	if privacy.Data.Sold && privacy.Consent.MessageIfSold != "" {
		view.Message = privacy.Consent.MessageIfSold
	}
	view.PolicyURL = privacy.Consent.Policy.URL
	if view.PolicyURL == "" || !strings.HasPrefix(view.PolicyURL, "/") || strings.HasPrefix(view.PolicyURL, "//") {
		view.PolicyURL = "/server/privacy"
	}
	view.PolicyText = privacy.Consent.Policy.Text
	if view.PolicyText == "" {
		view.PolicyText = i18n.Translate(i18n.LangFromContext(r.Context()), "nav.privacy")
	}
	view.AcceptText = privacy.Consent.Buttons.Accept
	if view.AcceptText == "" {
		view.AcceptText = i18n.Translate(i18n.LangFromContext(r.Context()), "cookie_consent.accept")
	}
	view.DeclineText = privacy.Consent.Buttons.Decline
	if view.DeclineText == "" {
		view.DeclineText = i18n.Translate(i18n.LangFromContext(r.Context()), "cookie_consent.decline")
	}
	view.ManageText = privacy.Consent.PreferencesText
	if view.ManageText == "" {
		view.ManageText = i18n.Translate(i18n.LangFromContext(r.Context()), "cookie_consent.manage_preferences")
	}
	if view.Message == "" {
		view.Message = i18n.T(r, "cookie_consent.message")
	}
	return view
}

func isI2PHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	return strings.HasSuffix(strings.TrimSuffix(host, "."), ".i2p")
}

func (s *Server) trackingNoscript(r *http.Request) template.HTML {
	if !s.trackingAllowed(r) || strings.ToLower(strings.TrimSpace(s.currentConfig().Server.Tracking.Type)) != "simple" {
		return ""
	}
	return template.HTML(`<noscript><img src="https://queue.simpleanalyticscdn.com/noscript.gif" alt="" referrerpolicy="no-referrer-when-downgrade"></noscript>`)
}

func trackingProviderFor(cfg config.TrackingConfig) trackingProvider {
	base := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	parsedBase, baseOK := parseTrackingURL(base)
	if base != "" && !baseOK {
		return trackingProvider{}
	}
	id, _ := json.Marshal(cfg.ID)
	quotedID := string(id)
	escapedID := template.HTMLEscapeString(cfg.ID)
	makeProvider := func(tags, body string, hosts ...string) trackingProvider {
		return trackingProvider{tags: template.HTML(tags), body: body, hosts: hosts}
	}
	selfHost := ""
	selfBase := ""
	if baseOK {
		selfHost = parsedBase.Scheme + "://" + parsedBase.Host
		selfBase = strings.TrimRight(parsedBase.String(), "/")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "google":
		if strings.HasPrefix(cfg.ID, "G-") {
			return makeProvider(`<script async src="https://www.googletagmanager.com/gtag/js?id=`+url.QueryEscape(cfg.ID)+`"></script>`, "window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments)}gtag('js',new Date());gtag('config',"+quotedID+");", "https://www.googletagmanager.com", "https://www.google-analytics.com")
		}
		if strings.HasPrefix(cfg.ID, "UA-") {
			return makeProvider(`<script async src="https://www.google-analytics.com/analytics.js"></script>`, "window.ga=window.ga||function(){(ga.q=ga.q||[]).push(arguments)};ga.l=+new Date;ga('create',"+quotedID+",'auto');ga('send','pageview');", "https://www.google-analytics.com")
		}
	case "matomo", "piwik":
		if selfHost != "" {
			return makeProvider("", "var _paq=window._paq=window._paq||[];_paq.push(['trackPageView']);_paq.push(['enableLinkTracking']);_paq.push(['setTrackerUrl',"+string(mustJSON(selfBase+"/matomo.php"))+"]);_paq.push(['setSiteId',"+quotedID+"]);var s=document.createElement('script');s.async=true;s.src="+string(mustJSON(selfBase+"/matomo.js"))+";document.head.appendChild(s);", selfHost)
		}
	case "owa":
		if selfHost != "" {
			return makeProvider("", "var owa_baseUrl="+string(mustJSON(selfBase+"/"))+";var owa_cmds=window.owa_cmds=window.owa_cmds||[];owa_cmds.push(['setSiteId',"+quotedID+"]);owa_cmds.push(['trackPageView']);owa_cmds.push(['trackClicks']);var s=document.createElement('script');s.async=true;s.src=owa_baseUrl+'modules/base/js/owa.tracker-combined-min.js';document.head.appendChild(s);", selfHost)
		}
	case "fathom":
		src, host := "https://cdn.usefathom.com/script.js", "https://cdn.usefathom.com"
		if selfHost != "" {
			src, host = selfBase+"/tracker.js", selfHost
		}
		if cfg.ID != "" {
			return makeProvider(`<script defer src="`+template.HTMLEscapeString(src)+`" data-site="`+escapedID+`"></script>`, "", host)
		}
	case "plausible":
		src, host := "https://plausible.io/js/script.js", "https://plausible.io"
		if selfHost != "" {
			src, host = selfBase+"/js/script.js", selfHost
		}
		if cfg.ID != "" {
			return makeProvider(`<script defer data-domain="`+escapedID+`" src="`+template.HTMLEscapeString(src)+`"></script>`, "", host)
		}
	case "umami":
		if selfHost != "" && cfg.ID != "" {
			return makeProvider(`<script defer src="`+template.HTMLEscapeString(selfBase+"/script.js")+`" data-website-id="`+escapedID+`"></script>`, "", selfHost)
		}
	case "simple":
		return makeProvider(`<script async defer src="https://scripts.simpleanalyticscdn.com/latest.js"></script>`, "", "https://scripts.simpleanalyticscdn.com", "https://queue.simpleanalyticscdn.com")
	case "cloudflare":
		if cfg.ID != "" {
			return makeProvider(`<script defer src="https://static.cloudflareinsights.com/beacon.min.js" data-cf-beacon='{"token":"`+escapedID+`"}'></script>`, "", "https://static.cloudflareinsights.com")
		}
	}
	return trackingProvider{}
}

func mustJSON(value string) []byte {
	b, _ := json.Marshal(value)
	return b
}

func parseTrackingURL(value string) (*url.URL, bool) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, false
	}
	return u, true
}

func (s *Server) handleTrackingJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s == nil || s.currentConfig() == nil || s.isTorRequest(r) || isI2PHost(r.Host) || !s.trackingAllowed(r) {
		http.NotFound(w, r)
		return
	}
	provider := trackingProviderFor(s.currentConfig().Server.Tracking)
	if provider.body == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(provider.body))
}

func (s *Server) handleConsent(w http.ResponseWriter, r *http.Request) {
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r, "errors.invalid_form"), http.StatusBadRequest)
		return
	}
	choice := r.FormValue("choice")
	previous, _ := readConsent(r)
	state := consentState{Essential: true, Timestamp: time.Now().Unix()}
	switch choice {
	case "accept":
		state.Preferences, state.Analytics = true, true
	case "decline":
	case "save":
		state.Preferences = r.FormValue("consent_preferences") == "on"
		state.Analytics = r.FormValue("consent_analytics") == "on"
	default:
		http.Error(w, i18n.T(r, "errors.invalid_consent_choice"), http.StatusBadRequest)
		return
	}
	value, err := json.Marshal(state)
	if err != nil {
		http.Error(w, i18n.T(r, "errors.save_consent_failed"), http.StatusInternalServerError)
		return
	}
	setPreferenceCookie(w, r, "cookie_consent", encodeConsentCookie(value), s)
	s.applyConsentWithdrawal(w, r, previous, state, choice == "decline")
	s.auditConsentChange(r, "cookie_consent", map[string]bool{"essential": true, "preferences": state.Preferences, "analytics": state.Analytics})
	redirectPreference(w, safePreferenceRedirect(s, r, "/"))
}

func (s *Server) applyConsentWithdrawal(w http.ResponseWriter, r *http.Request, previous, next consentState, declined bool) {
	preferencesWithdrawn := previous.Preferences && !next.Preferences
	analyticsWithdrawn := previous.Analytics && !next.Analytics
	if !declined && !preferencesWithdrawn && !analyticsWithdrawn {
		return
	}
	if preferencesWithdrawn {
		for _, cookie := range r.Cookies() {
			if cookie.Name == "theme" || cookie.Name == "lang" || strings.HasPrefix(cookie.Name, preferencePrefix) {
				expirePreferenceCookie(w, r, cookie.Name, s)
			}
		}
	}
	cfg := s.currentConfig()
	if cfg == nil || !cfg.Web.Headers.ClearSiteData.OnConsentWithdrawal {
		return
	}
	value := `"cache", "storage"`
	if cfg.Web.Headers.ClearSiteData.ExecutionContexts {
		value = `"cache", "storage", "executionContexts"`
	}
	w.Header().Set("Clear-Site-Data", value)
}

func (s *Server) setTokenRevocationClearSiteData(w http.ResponseWriter) {
	cfg := s.currentConfig()
	if cfg == nil || !cfg.Web.Headers.ClearSiteData.OnTokenRevocation {
		return
	}
	value := `"cache", "storage"`
	if cfg.Web.Headers.ClearSiteData.ExecutionContexts {
		value = `"cache", "storage", "executionContexts"`
	}
	w.Header().Set("Clear-Site-Data", value)
}

func (s *Server) auditConsentChange(r *http.Request, consentType string, value any) {
	if s == nil || r == nil || s.config == nil || s.config.Paths == nil {
		return
	}
	actor := audit.Actor{Type: "anonymous", IP: consentAuditIP(s, r)}
	for _, cookie := range ownerCookies(r) {
		identity, err := s.AuthenticateBearerToken(cookie.Value)
		if err == nil && identity.OwnerID != "" {
			actor.Type, actor.ID = "owner", identity.OwnerID
			break
		}
	}
	version := ""
	if cfg := s.currentConfig(); cfg != nil {
		version = cfg.Server.Privacy.Consent.Policy.Version
	}
	if err := audit.Append(s.config.Paths.GetLogsDir(), audit.Entry{
		Event: "compliance.consent_updated", Category: "compliance", Severity: "info",
		Actor: actor, Details: map[string]any{"consent_type": consentType, "new_value": value, "policy_version": version}, Result: "success",
	}); err != nil {
		log.Printf("audit event compliance.consent_updated could not be appended")
	}
}

func consentAuditIP(s *Server, r *http.Request) string {
	if identity, ok := r.Context().Value(torRequestContextKey{}).(torRequestIdentity); ok {
		if identity.circuitID == "" || identity.circuitID == "tor" {
			return "tor"
		}
		return "tor:" + identity.circuitID
	}
	address, err := netip.ParseAddr(s.clientIP(r))
	if err != nil {
		return ""
	}
	address = address.Unmap()
	bits := 48
	if address.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(address, bits).Masked().Addr().String()
}

func (s *Server) trackingCSP(r *http.Request, base string) string {
	tlsActive := r.TLS != nil || s.currentConfig() != nil && s.currentConfig().Server.SSL.Enabled
	if s.isTorRequest(r) {
		tlsActive = false
	}
	policy := cspDirectivesForCaptcha(tlsActive, base, s.contactCaptchaProviderForRequest(r))
	if !s.trackingAllowed(r) {
		return policy
	}
	directives := strings.Split(policy, "; ")
	for _, host := range trackingProviderFor(s.currentConfig().Server.Tracking).hosts {
		directives = appendCSPHost(directives, "script-src", host)
		directives = appendCSPHost(directives, "connect-src", host)
	}
	return strings.Join(directives, "; ")
}

func appendCSPHost(directives []string, name, host string) []string {
	for index, directive := range directives {
		if strings.HasPrefix(directive, name+" ") && !strings.Contains(" "+directive+" ", " "+host+" ") {
			directives[index] = directive + " " + host
			return directives
		}
	}
	return directives
}
