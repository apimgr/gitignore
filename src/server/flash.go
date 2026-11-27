package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/db"
)

const flashCookieName = "gitignore_flash"

type flashPayload struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Expires int64  `json:"expires"`
}

var flashKeys = map[string]bool{
	"web.flash.action_completed":       true,
	"web.flash.announcement_dismissed": true,
	"web.flash.consent_required":       true,
	"web.flash.consent_saved":          true,
	"web.flash.contact_sent":           true,
	"web.flash.preferences_saved":      true,
	"web.flash.privacy_choice_saved":   true,
	"web.flash.theme_saved":            true,
	"web.flash.token_action_completed": true,
}

func flashSigningKey() ([]byte, error) {
	return db.ApplicationSecret("cookie_signing_key")
}

func setFlashCookie(w http.ResponseWriter, key, kind string, secure bool) {
	if !flashKeys[key] || (kind != "success" && kind != "info" && kind != "warning" && kind != "error") {
		return
	}
	secret, err := flashSigningKey()
	if err != nil {
		return
	}
	payload, err := json.Marshal(flashPayload{Key: key, Type: kind, Expires: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		return
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	value := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name: flashCookieName, Value: value, Path: "/", MaxAge: 60,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func consumeFlashCookie(w http.ResponseWriter, r *http.Request, secure bool) (string, string) {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return "", ""
	}
	http.SetCookie(w, &http.Cookie{
		Name: flashCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return "", ""
	}
	secret, err := flashSigningKey()
	if err != nil {
		return "", ""
	}
	supplied, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0]))
	if subtle.ConstantTimeCompare(supplied, mac.Sum(nil)) != 1 {
		return "", ""
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ""
	}
	var payload flashPayload
	if json.Unmarshal(encoded, &payload) != nil || payload.Expires < time.Now().Unix() || !flashKeys[payload.Key] {
		return "", ""
	}
	if payload.Type != "success" && payload.Type != "info" && payload.Type != "warning" && payload.Type != "error" {
		return "", ""
	}
	return payload.Key, payload.Type
}

func flashRedirectMiddleware(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := strings.ToLower(r.Header.Get("Content-Type"))
		formPost := r.Method == http.MethodPost && (strings.HasPrefix(contentType, "application/x-www-form-urlencoded") || strings.HasPrefix(contentType, "multipart/form-data"))
		if !formPost || r.Header.Get("X-Requested-With") != "" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&flashResponseWriter{ResponseWriter: w, request: r, server: s}, r)
	})
}

type flashResponseWriter struct {
	http.ResponseWriter
	request *http.Request
	server  *Server
	wrote   bool
}

func (w *flashResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if status == http.StatusSeeOther && w.request.FormValue("action") != "reset" && !hasFlashCookie(w.Header().Values("Set-Cookie")) {
		key, kind := flashForRedirect(w.request, w.Header().Get("Location"))
		setFlashCookie(w.ResponseWriter, key, kind, preferenceCookieSecure(w.server, w.request))
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *flashResponseWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *flashResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func hasFlashCookie(cookies []string) bool {
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie, flashCookieName+"=") {
			return true
		}
	}
	return false
}

func flashForRedirect(r *http.Request, location string) (string, string) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/server/contact"):
		if strings.Contains(location, "sent=1") || strings.Contains(location, "report=") {
			return "", ""
		}
		return "web.flash.contact_sent", "success"
	case strings.HasSuffix(path, "/server/consent"):
		return "web.flash.consent_saved", "success"
	case strings.HasSuffix(path, "/server/ccpa"):
		return "web.flash.privacy_choice_saved", "success"
	case strings.HasSuffix(path, "/server/theme"):
		return "web.flash.theme_saved", "success"
	case strings.HasSuffix(path, "/announcements/dismiss"):
		return "web.flash.announcement_dismissed", "success"
	case strings.Contains(path, "/server/preferences/tokens"):
		return "web.flash.token_action_completed", "success"
	case strings.HasSuffix(path, "/server/preferences") && strings.Contains(location, "consent_required=1"):
		return "web.flash.consent_required", "warning"
	case strings.HasSuffix(path, "/server/preferences"):
		return "web.flash.preferences_saved", "success"
	default:
		return "web.flash.action_completed", "info"
	}
}
