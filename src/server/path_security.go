package server

import (
	"context"
	"net/http"
	"path"
	"strings"

	"github.com/apimgr/gitignore/src/common/i18n"
)

type originalPathKey struct{}

// urlNormalizeMiddleware collapses duplicate slashes and dot segments while
// retaining the incoming path for the following security check.
func urlNormalizeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original := r.URL.Path
		r = r.WithContext(context.WithValue(r.Context(), originalPathKey{}, original))
		cleaned := path.Clean(original)
		if !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		if original != "/" && strings.HasSuffix(original, "/") && !strings.HasSuffix(cleaned, "/") {
			cleaned += "/"
		}
		r.URL.Path = cleaned
		next.ServeHTTP(w, r)
	})
}

// pathSecurityMiddleware rejects traversal before authentication and routing.
func pathSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original, _ := r.Context().Value(originalPathKey{}).(string)
		if original == "" {
			original = r.URL.Path
		}
		rawPath := r.URL.RawPath
		if rawPath == "" {
			rawPath = original
		}
		if strings.Contains(original, "..") || strings.Contains(rawPath, "..") || strings.Contains(strings.ToLower(rawPath), "%2e") {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.bad_request"), http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
