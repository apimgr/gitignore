package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/apimgr/gitignore/src/common/i18n"
)

const preferencePrefix = "gitignore_pref_"

type preferenceState struct {
	Theme   string            `json:"theme"`
	Lang    string            `json:"lang"`
	Prefs   map[string]string `json:"prefs"`
	URL     string            `json:"url,omitempty"`
	Code    string            `json:"code,omitempty"`
	Applied []string          `json:"-"`
	Ignored []string          `json:"-"`
}

func preferenceCookieSecure(s *Server, r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(s.detectServerURL(r)), "https://")
}

func validPreferenceLanguage(value string) string {
	for _, language := range i18n.SupportedLanguages() {
		if strings.EqualFold(language, value) {
			return language
		}
	}
	base := strings.SplitN(value, "-", 2)[0]
	for _, language := range i18n.SupportedLanguages() {
		if strings.EqualFold(language, base) {
			return language
		}
	}
	return ""
}

func (s *Server) currentPreferenceState(r *http.Request) preferenceState {
	state := preferenceState{Theme: themeFromRequest(r), Lang: i18n.LangFromContext(r.Context()), Prefs: map[string]string{}}
	if state.Lang == "" {
		state.Lang = "en"
	}
	for _, cookie := range r.Cookies() {
		if cookie.Name == "lang" {
			if lang := validPreferenceLanguage(cookie.Value); lang != "" {
				state.Lang = lang
			}
		} else if strings.HasPrefix(cookie.Name, preferencePrefix) {
			key := strings.TrimPrefix(cookie.Name, preferencePrefix)
			if validPreferenceKey(key) && validPreferenceValue(cookie.Value) {
				state.Prefs[key] = cookie.Value
			}
		}
	}
	return state
}

func validPreferenceKey(key string) bool {
	if key == "" || len(key) > 64 || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, ch := range key[1:] {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func validPreferenceValue(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && !strings.ContainsRune("_.-", ch) {
			return false
		}
	}
	return true
}

func (s *Server) preferenceExport(r *http.Request) preferenceState {
	state := s.currentPreferenceState(r)
	values := url.Values{"theme": {state.Theme}, "lang": {state.Lang}}
	for key, value := range state.Prefs {
		values.Set(preferencePrefix+key, value)
	}
	query := values.Encode()
	state.URL = s.publicServerURL(r) + "/server/preferences/sync/import?" + query
	state.Code = base64.RawURLEncoding.EncodeToString([]byte(query))
	return state
}

func preferenceInput(r *http.Request, form bool, codeWins bool) url.Values {
	direct := url.Values{}
	if form {
		for key, values := range r.Form {
			if key != "code" && key != "csrf_token" && key != "redirect" && key != "action" && key != "consent_form" && key != "consent_preferences" && key != "consent_analytics" && key != "ccpa_opt_out" {
				direct[key] = values
			}
		}
	} else {
		for key, values := range r.URL.Query() {
			if key != "code" {
				direct[key] = values
			}
		}
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if len(code) > 2048 {
		return direct
	}
	if q := strings.IndexByte(code, '?'); q >= 0 {
		code = code[q+1:]
	}
	var parsed url.Values
	plainQuery := strings.Contains(code, "=") && !looksLikePaddedBase64URL(code)
	if plainQuery {
		var err error
		parsed, err = url.ParseQuery(code)
		if err != nil {
			parsed = nil
		}
	} else if code != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(code)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(code)
		}
		if err == nil {
			parsed, err = url.ParseQuery(string(decoded))
			if err != nil {
				parsed = nil
			}
		}
	}
	for key, values := range parsed {
		if codeWins {
			direct[key] = values
		} else if _, exists := direct[key]; !exists {
			direct[key] = values
		}
	}
	return direct
}

func looksLikePaddedBase64URL(code string) bool {
	if strings.ContainsAny(code, "&=") && !strings.HasSuffix(code, "=") {
		return false
	}
	trimmed := strings.TrimRight(code, "=")
	padding := len(code) - len(trimmed)
	if padding == 0 || padding > 2 || trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}

var preferenceControlFields = map[string]bool{
	"csrf_token": true, "code": true, "redirect": true, "action": true,
	"consent_form": true, "consent_preferences": true, "consent_analytics": true, "ccpa_opt_out": true,
}

func (s *Server) applyPreferences(w http.ResponseWriter, r *http.Request, values url.Values, reset bool) preferenceState {
	state := s.currentPreferenceState(r)
	state.Applied = []string{}
	state.Ignored = []string{}
	if reset {
		for _, cookie := range r.Cookies() {
			if cookie.Name == "theme" || cookie.Name == "lang" || cookie.Name == "cookie_consent" || cookie.Name == "ccpa_opt_out" || cookie.Name == "gitignore_build" || cookie.Name == dismissedAnnouncementsCookie || strings.HasPrefix(cookie.Name, preferencePrefix) {
				expirePreferenceCookie(w, r, cookie.Name, s)
			}
		}
		w.Header().Set("Clear-Site-Data", `"storage"`)
		state.Theme, state.Lang, state.Prefs = "dark", "en", map[string]string{}
		return state
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 32 {
		keys = keys[:32]
	}
	for _, key := range keys {
		if preferenceControlFields[key] {
			continue
		}
		vals := values[key]
		if len(vals) == 0 || len(vals[0]) > 64 {
			state.Ignored = append(state.Ignored, key)
			continue
		}
		value := vals[0]
		cookieName := key
		switch key {
		case "theme":
			if !validThemes[value] {
				state.Ignored = append(state.Ignored, key)
				continue
			}
			state.Theme = value
		case "lang":
			value = validPreferenceLanguage(value)
			if value == "" {
				state.Ignored = append(state.Ignored, key)
				continue
			}
			state.Lang = value
		case "cookie_consent", "ccpa_opt_out", "gitignore_build", "owner_token", "csrf_token", "session":
			state.Ignored = append(state.Ignored, key)
			continue
		default:
			if !strings.HasPrefix(key, preferencePrefix) || !validPreferenceKey(strings.TrimPrefix(key, preferencePrefix)) || !validPreferenceValue(value) {
				state.Ignored = append(state.Ignored, key)
				continue
			}
			prefKey := strings.TrimPrefix(key, preferencePrefix)
			state.Prefs[prefKey] = value
		}
		setPreferenceCookie(w, r, cookieName, value, s)
		state.Applied = append(state.Applied, key)
	}
	return state
}

func setPreferenceCookie(w http.ResponseWriter, r *http.Request, name, value string, s *Server) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 31536000, SameSite: http.SameSiteLaxMode, Secure: preferenceCookieSecure(s, r)})
}

func expirePreferenceCookie(w http.ResponseWriter, r *http.Request, name string, s *Server) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode, Secure: preferenceCookieSecure(s, r)})
}

func safePreferenceRedirect(s *Server, r *http.Request, fallback string) string {
	valid := func(target string) bool {
		return strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") && !strings.HasPrefix(target, "/\\") && !strings.ContainsAny(target, "\r\n") && !strings.HasPrefix(target, "/server/preferences")
	}
	if target := r.FormValue("redirect"); valid(target) {
		return target
	}
	if referer := r.Referer(); referer != "" {
		parsed, err := url.Parse(referer)
		publicURL, publicErr := url.Parse(s.publicServerURL(r))
		if err == nil && publicErr == nil && parsed.IsAbs() && strings.EqualFold(parsed.Scheme, publicURL.Scheme) && strings.EqualFold(parsed.Host, publicURL.Host) && valid(parsed.RequestURI()) {
			return parsed.RequestURI()
		}
	}
	return fallback
}

func writePreferenceAPI(w http.ResponseWriter, state preferenceState) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	data := map[string]interface{}{"theme": state.Theme, "lang": state.Lang, "prefs": state.Prefs}
	if state.URL != "" {
		data["url"] = state.URL
	}
	if state.Code != "" {
		data["code"] = state.Code
	}
	if state.Applied != nil {
		data["applied"] = state.Applied
		data["ignored"] = state.Ignored
	}
	_ = json.NewEncoder(w).Encode(APIResponse{OK: true, Data: data})
}

func isJSONRequest(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") || strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
}

func parsePreferenceJSON(r *http.Request) (url.Values, error) {
	var body map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	values := url.Values{}
	for key, value := range body {
		if text, ok := value.(string); ok {
			values.Set(key, text)
		} else {
			values.Set(key, fmt.Sprint(value))
		}
	}
	return values, nil
}

func (s *Server) handlePreferencesSyncPage(w http.ResponseWriter, r *http.Request) {
	state := s.preferenceExport(r)
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") {
		writePreferenceAPI(w, state)
		return
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/plain") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, "%s: %s\n%s: %s\n", i18n.T(r, "preferences_page.export_url"), state.URL,
			i18n.T(r, "preferences_page.export_code"), state.Code)
		return
	}
	s.renderPage(w, r, "preferences_sync", PageData{Title: i18n.T(r, "preferences_page.sync_title"), Data: map[string]interface{}{"url": state.URL, "code": state.Code, "cancelURL": "/server/preferences"}})
}

func (s *Server) handlePreferencesSyncImport(w http.ResponseWriter, r *http.Request) {
	values := preferenceInput(r, false, false)
	state := s.applyPreferences(w, r, values, false)
	if isJSONRequest(r) {
		writePreferenceAPI(w, state)
		return
	}
	redirectPreference(w, safePreferenceRedirect(s, r, "/"))
}

func (s *Server) handlePreferencesSyncSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !validCSRFRequest(r) {
		sendAPIResponseErrorLocalized(w, r, "CSRF_FAILED")
		return
	}
	if err := r.ParseForm(); err != nil {
		r.Form = url.Values{}
	}
	values := preferenceInput(r, true, false)
	s.applyPreferences(w, r, values, false)
	redirectPreference(w, safePreferenceRedirect(s, r, "/server/preferences/sync"))
}

func redirectPreference(w http.ResponseWriter, target string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
}

func (s *Server) handleAPIPreferences(w http.ResponseWriter, r *http.Request) {
	writePreferenceAPI(w, s.currentPreferenceState(r))
}

func (s *Server) handleAPIPreferencesSync(w http.ResponseWriter, r *http.Request) {
	writePreferenceAPI(w, s.preferenceExport(r))
}

func (s *Server) handleAPIPreferencesSyncImport(w http.ResponseWriter, r *http.Request) {
	values := preferenceInput(r, false, false)
	writePreferenceAPI(w, s.applyPreferences(w, r, values, false))
}

func (s *Server) handleAPIPreferencesSave(w http.ResponseWriter, r *http.Request) {
	var values url.Values
	var err error
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		values, err = parsePreferenceJSON(r)
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		err = r.ParseForm()
		values = r.Form
	}
	if err != nil {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	state := s.applyPreferences(w, r, values, false)
	if len(state.Applied) == 0 {
		sendAPIResponseErrorLocalized(w, r, "VALIDATION_FAILED")
		return
	}
	writePreferenceAPI(w, state)
}
