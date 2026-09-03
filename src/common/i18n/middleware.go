package i18n

import (
	"context"
	"net/http"
)

// ctxKey is the private context key type under which the resolved request
// language is stored, so no other package can collide with it.
type ctxKey struct{}

var langKey = ctxKey{}

// cookieName and cookieMaxAge back the language persistence cookie. They
// default to the AI.md PART 30 values ("lang", one year) and may be overridden
// once at startup via Configure from the server's config.server.i18n block.
var (
	cookieName   = "lang"
	cookieMaxAge = 365 * 24 * 60 * 60
)

// Configure overrides the language-cookie name and max-age from configuration.
// A non-positive maxAge or empty name leaves the corresponding default in
// place. It must be called before the server starts serving; it is not safe
// to call concurrently with in-flight requests.
func Configure(name string, maxAgeSeconds int) {
	if name != "" {
		cookieName = name
	}
	if maxAgeSeconds > 0 {
		cookieMaxAge = maxAgeSeconds
	}
}

// Middleware resolves the request language (?lang= → cookie → Accept-Language
// → en), persists a ?lang= choice via a one-year cookie, and stashes the
// result in the request context for downstream handlers (AI.md PART 30
// "Language Selection via Query Parameter").
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A ?lang= that is supported is persisted so subsequent requests keep
		// the language without the query param (shareable links).
		if q := r.URL.Query().Get("lang"); q != "" && IsSupported(q) {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    normalize(q),
				Path:     "/",
				MaxAge:   cookieMaxAge,
				SameSite: http.SameSiteLaxMode,
				Secure:   r.TLS != nil,
				HttpOnly: true,
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
