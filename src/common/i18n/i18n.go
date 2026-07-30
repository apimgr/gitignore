// Package i18n is the single source of truth for this project's translations,
// shared by every binary (server and CLI) via go:embed (see AI.md PART 30
// "I18N & A11Y"). Translation files live in locales/*.json and are compiled
// into each binary — there is no runtime filesystem dependency and no partial
// language support: if the server ships a language, the CLI ships it too.
//
// Lookup rules (AI.md PART 30):
//   - Keys are dot-separated lowercase paths into the JSON tree (health.title).
//   - An unsupported language silently falls back to English (en) — never an
//     error or panic.
//   - A key missing in the active language falls back to the English value;
//     if English also lacks it, the key itself is returned as a last resort.
package i18n

import (
	"embed"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// localeFS embeds every locale file. The path is relative to this package.
//
//go:embed locales/*.json
var localeFS embed.FS

// DefaultLang is the fallback language used whenever detection fails or a
// requested language is unsupported (AI.md PART 30 "Default language").
const DefaultLang = "en"

// supportedOrder is the canonical order languages are offered in the WebUI
// selector and the CLI. It matches the "Supported Languages" table in
// AI.md PART 30.
var supportedOrder = []string{"en", "es", "zh", "fr", "ar", "de", "ja"}

// LanguageInfo describes one supported language for the selector UI and the
// html dir attribute.
type LanguageInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"native_name"`
	Direction  string `json:"direction"`
}

// translations holds every language's flattened key/value pairs. It is built
// once at init from the embedded JSON and never mutated afterwards, so it is
// safe for concurrent reads without locking.
var (
	translations = map[string]map[string]string{}
	rawLocales   = map[string][]byte{}
	metaByLang   = map[string]LanguageInfo{}
)

func init() {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		// An embed failure is a build-time bug; leave translations empty so
		// Translate falls through to returning the key itself.
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		data, err := localeFS.ReadFile("locales/" + e.Name())
		if err != nil {
			continue
		}
		var tree map[string]interface{}
		if err := json.Unmarshal(data, &tree); err != nil {
			continue
		}
		flat := map[string]string{}
		flatten("", tree, flat)
		translations[lang] = flat
		rawLocales[lang] = data
		metaByLang[lang] = LanguageInfo{
			Code:       lang,
			Name:       flat["meta.name"],
			NativeName: flat["meta.native_name"],
			Direction:  flat["meta.direction"],
		}
	}
}

// flatten collapses a nested JSON object into dot-separated keys so that
// {"a":{"b":"c"}} becomes {"a.b":"c"}. Non-string leaves are stringified.
func flatten(prefix string, node map[string]interface{}, out map[string]string) {
	for k, v := range node {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			flatten(key, val, out)
		case string:
			out[key] = val
		case float64:
			out[key] = strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			out[key] = strconv.FormatBool(val)
		}
	}
}

// IsSupported reports whether lang is one of the compiled-in languages.
func IsSupported(lang string) bool {
	_, ok := translations[strings.ToLower(strings.TrimSpace(lang))]
	return ok
}

// SupportedLanguages returns the supported language codes in canonical order.
func SupportedLanguages() []string {
	out := make([]string, 0, len(supportedOrder))
	for _, c := range supportedOrder {
		if IsSupported(c) {
			out = append(out, c)
		}
	}
	// Include any embedded language not in the canonical order (defensive).
	for lang := range translations {
		if !contains(out, lang) {
			out = append(out, lang)
		}
	}
	return out
}

// AvailableLanguages returns metadata for every supported language, in
// canonical order, for rendering the language selector.
func AvailableLanguages() []LanguageInfo {
	codes := SupportedLanguages()
	out := make([]LanguageInfo, 0, len(codes))
	for _, c := range codes {
		if info, ok := metaByLang[c]; ok {
			out = append(out, info)
		}
	}
	return out
}

// Direction returns the text direction ("ltr" or "rtl") for lang, falling
// back to "ltr".
func Direction(lang string) string {
	if info, ok := metaByLang[normalize(lang)]; ok && info.Direction != "" {
		return info.Direction
	}
	return "ltr"
}

// LocaleJSON returns the raw embedded JSON for lang, used to serve
// /locales/{lang}.json to the frontend. The bool is false for unsupported
// languages.
func LocaleJSON(lang string) ([]byte, bool) {
	data, ok := rawLocales[normalize(lang)]
	return data, ok
}

// Translate returns the translated string for key in lang. An unsupported
// lang uses English; a missing key falls back to the English value and then
// to the key itself (AI.md PART 30 "Missing key fallback").
func Translate(lang, key string) string {
	lang = normalize(lang)
	if !IsSupported(lang) {
		lang = DefaultLang
	}
	if val, ok := translations[lang][key]; ok {
		return val
	}
	if val, ok := translations[DefaultLang][key]; ok {
		return val
	}
	return key
}

// TranslateFormat translates key and interpolates named {placeholders}. The
// variadic args accept either a single map (map[string]string or
// map[string]interface{}) or an even number of alternating key/value pairs,
// matching the tf(lang, key, args...) template helper in AI.md PART 30.
func TranslateFormat(lang, key string, args ...interface{}) string {
	return interpolate(Translate(lang, key), buildVars(args...))
}

// TranslatePlural selects the CLDR plural form for count under lang's rules
// and returns the interpolated string, replacing {count} with the number
// (AI.md PART 30 "Plural Rules").
func TranslatePlural(lang, key string, count int) string {
	lang = normalize(lang)
	if !IsSupported(lang) {
		lang = DefaultLang
	}
	// Explicit zero: when count is 0 and the key defines a "zero" form, prefer
	// it for friendlier output ("No items") even in languages whose CLDR rules
	// map 0 to "other" (e.g. English). Falls back to the English zero form.
	if count == 0 {
		if val, ok := translations[lang][key+".zero"]; ok {
			return interpolate(val, map[string]string{"count": strconv.Itoa(count)})
		}
		if val, ok := translations[DefaultLang][key+".zero"]; ok {
			return interpolate(val, map[string]string{"count": strconv.Itoa(count)})
		}
	}
	form := pluralForm(lang, count)
	val, ok := translations[lang][key+"."+form]
	if !ok {
		val, ok = translations[lang][key+".other"]
	}
	if !ok {
		val, ok = translations[DefaultLang][key+"."+form]
	}
	if !ok {
		val, ok = translations[DefaultLang][key+".other"]
	}
	if !ok {
		return key
	}
	return interpolate(val, map[string]string{"count": strconv.Itoa(count)})
}

// pluralForm returns the CLDR plural category ("zero","one","two","few",
// "many","other") for count under lang's rules.
func pluralForm(lang string, count int) string {
	tag, err := language.Parse(lang)
	if err != nil {
		tag = language.English
	}
	form := plural.Cardinal.MatchPlural(tag, count, 0, 0, 0, 0)
	switch form {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}

// interpolate replaces every {name} token in s with vars[name].
func interpolate(s string, vars map[string]string) string {
	if len(vars) == 0 || !strings.Contains(s, "{") {
		return s
	}
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// buildVars normalizes the variadic interpolation arguments into a flat
// string map.
func buildVars(args ...interface{}) map[string]string {
	vars := map[string]string{}
	if len(args) == 1 {
		switch m := args[0].(type) {
		case map[string]string:
			return m
		case map[string]interface{}:
			for k, v := range m {
				vars[k] = stringify(v)
			}
			return vars
		}
	}
	// Alternating key/value pairs.
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		vars[key] = stringify(args[i+1])
	}
	return vars
}

func stringify(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		return ""
	}
}

func normalize(lang string) string { return strings.ToLower(strings.TrimSpace(lang)) }

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Keys returns every translation key present in lang, sorted. Used by the
// build-time validator to compare key sets across languages.
func Keys(lang string) []string {
	m := translations[normalize(lang)]
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
