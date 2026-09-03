package i18n

import (
	"net/http"
	"os"
	"strconv"
	"strings"
)

// LangFromRequest resolves the request language following the AI.md PART 30
// fallback chain: ?lang= query param → lang cookie → Accept-Language header →
// English default. An unsupported value at any tier silently degrades to
// English; this function never returns an unsupported language.
func LangFromRequest(r *http.Request) string {
	// 1. Query param (highest priority; the middleware also persists a cookie).
	if lang := r.URL.Query().Get("lang"); lang != "" {
		if IsSupported(lang) {
			return normalize(lang)
		}
		return DefaultLang
	}
	// 2. Cookie.
	if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
		if IsSupported(cookie.Value) {
			return normalize(cookie.Value)
		}
		return DefaultLang
	}
	// 3. Accept-Language header (best supported match).
	if accept := r.Header.Get("Accept-Language"); accept != "" {
		if lang := parseBestMatch(accept); lang != "" {
			return lang
		}
	}
	// 4. Default.
	return DefaultLang
}

// parseBestMatch parses an Accept-Language header and returns the highest
// q-weighted supported language, or "" when none match. Only the primary
// subtag is considered (es-419 → es), per AI.md PART 30's language table.
func parseBestMatch(header string) string {
	type candidate struct {
		lang string
		q    float64
	}
	var best candidate
	best.q = -1
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lang := part
		q := 1.0
		if semi := strings.Index(part, ";"); semi >= 0 {
			lang = strings.TrimSpace(part[:semi])
			for _, param := range strings.Split(part[semi+1:], ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					if v, err := strconv.ParseFloat(strings.TrimPrefix(param, "q="), 64); err == nil {
						q = v
					}
				}
			}
		}
		if lang == "*" {
			continue
		}
		primary := normalize(strings.SplitN(lang, "-", 2)[0])
		if !IsSupported(primary) {
			continue
		}
		if q > best.q {
			best = candidate{lang: primary, q: q}
		}
	}
	return best.lang
}

// ResolveCLILang resolves the output language for a CLI/server binary using
// the AI.md PART 30 priority: --lang flag → config file value → LC_ALL/LANG
// env → English. Any unsupported value silently falls back to English.
func ResolveCLILang(flagLang, configLang string) string {
	if flagLang != "" {
		return validateLang(flagLang)
	}
	if configLang != "" && normalize(configLang) != "auto" {
		return validateLang(configLang)
	}
	if v := os.Getenv("LC_ALL"); v != "" {
		return validateLang(localePrimary(v))
	}
	if v := os.Getenv("LANG"); v != "" {
		return validateLang(localePrimary(v))
	}
	return DefaultLang
}

// localePrimary extracts the language subtag from a POSIX locale string such
// as "es_ES.UTF-8" → "es".
func localePrimary(locale string) string {
	locale = strings.TrimSpace(locale)
	if i := strings.IndexAny(locale, "_.@"); i >= 0 {
		locale = locale[:i]
	}
	return locale
}

// validateLang returns lang if supported, otherwise English. It never errors.
func validateLang(lang string) string {
	lang = normalize(lang)
	if IsSupported(lang) {
		return lang
	}
	return DefaultLang
}
