package i18n

import (
	"context"
	"net/http"
)

// ctxKey is the private context key type under which the resolved request
// language is stored, so no other package can collide with it.
type ctxKey struct{}

var langKey = ctxKey{}

const (
	cookieName   = "lang"
	cookieMaxAge = 365 * 24 * 60 * 60
)

// SetLanguageCookie persists an explicit language choice using the configured
// cookie name and lifetime.
func SetLanguageCookie(w http.ResponseWriter, r *http.Request, lang string) bool {
	if !IsSupported(lang) {
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: normalize(lang), Path: "/", MaxAge: cookieMaxAge, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	return true
}

// ClearLanguageCookie removes an explicit language override and restores
// automatic Accept-Language detection.
func ClearLanguageCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
}

// LanguagePreference returns the explicit saved language, or "auto" when the
// visitor is using automatic detection.
func LanguagePreference(r *http.Request) string {
	if cookie, err := r.Cookie(cookieName); err == nil && IsSupported(cookie.Value) {
		return normalize(cookie.Value)
	}
	return "auto"
}

// Middleware resolves the request language (?lang= → cookie → Accept-Language
// → en), persists a ?lang= choice via a one-year cookie, and stashes the
// result in the request context for downstream handlers (AI.md PART 30
// "Language Selection via Query Parameter").
func Middleware(next http.Handler) http.Handler {
	return MiddlewareWithSecure(next, func(r *http.Request) bool { return r.TLS != nil })
}

// MiddlewareWithSecure resolves the language and persists explicit query
// choices. The server supplies its trusted-proxy-aware transport predicate.
func MiddlewareWithSecure(next http.Handler, secure func(*http.Request) bool) http.Handler {
	return MiddlewareWithSecureAndConsent(next, secure, func(*http.Request) bool { return true })
}

// MiddlewareWithSecureAndConsent resolves request languages and persists
// query-selected preferences only when the caller's consent check allows it.
func MiddlewareWithSecureAndConsent(next http.Handler, secure func(*http.Request) bool, canPersist func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A ?lang= that is supported is persisted so subsequent requests keep
		// the language without the query param (shareable links).
		if q := r.URL.Query().Get("lang"); q != "" && IsSupported(q) && canPersist(r) {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    normalize(q),
				Path:     "/",
				MaxAge:   cookieMaxAge,
				SameSite: http.SameSiteLaxMode,
				Secure:   secure(r),
			})
		}
		lang := LangFromRequest(r)
		ctx := context.WithValue(r.Context(), langKey, lang)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LangFromContext returns the language resolved by Middleware, or English if
// the middleware did not run (defensive; never returns unsupported).
func LangFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(langKey).(string); ok && IsSupported(lang) {
		return lang
	}
	return DefaultLang
}

// T is a request-scoped translation helper: T(r, "errors.not_found").
func T(r *http.Request, key string) string {
	return Translate(LangFromContext(r.Context()), key)
}

// TF is a request-scoped formatted translation helper.
func TF(r *http.Request, key string, args ...interface{}) string {
	return TranslateFormat(LangFromContext(r.Context()), key, args...)
}
