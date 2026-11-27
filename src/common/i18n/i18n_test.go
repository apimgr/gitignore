package i18n

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var interpolationPattern = regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`)

// TestKeyConsistency checks key parity, non-empty values, and interpolation
// parity for every embedded locale.
func TestKeyConsistency(t *testing.T) {
	base := translations[DefaultLang]
	baseKeys := messageKeys(base)
	for _, lang := range SupportedLanguages() {
		locale := translations[lang]
		keys := messageKeys(locale)
		if len(keys) != len(baseKeys) {
			t.Errorf("%s: has %d message keys; en has %d", lang, len(keys), len(baseKeys))
		}
		for key := range baseKeys {
			if strings.HasPrefix(key, "plurals.") {
				checkPluralMessage(t, lang, key, base, locale)
				continue
			}
			english := base[key]
			value, ok := locale[key]
			if !ok {
				t.Errorf("%s: missing key %q", lang, key)
				continue
			}
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: empty value for %q", lang, key)
			}
			if !samePlaceholders(english, value) {
				t.Errorf("%s: interpolation placeholders differ for %q", lang, key)
			}
		}
		for key := range keys {
			if _, exists := baseKeys[key]; !exists {
				t.Errorf("%s: orphaned key %q", lang, key)
			}
		}
	}
}

func messageKeys(values map[string]string) map[string]bool {
	keys := make(map[string]bool, len(values))
	for key := range values {
		if strings.HasPrefix(key, "plurals.") {
			parts := strings.Split(key, ".")
			if len(parts) == 3 {
				switch parts[2] {
				case "zero", "one", "two", "few", "many", "other":
					key = strings.Join(parts[:2], ".")
				}
			}
		}
		keys[key] = true
	}
	return keys
}

func checkPluralMessage(t *testing.T, lang, key string, english, locale map[string]string) {
	t.Helper()
	wanted := map[string][]string{
		"en": {"one", "other"}, "es": {"one", "other"}, "fr": {"one", "other"},
		"de": {"one", "other"}, "it": {"one", "many", "other"},
		"zh": {"other"}, "ja": {"other"},
		"ar": {"zero", "one", "two", "few", "many", "other"},
	}[lang]
	baseCategories := pluralCategories(english, key)
	actualCategories := pluralCategories(locale, key)
	optionalZero := actualCategories["zero"] && !containsString(wanted, "zero")
	for _, category := range wanted {
		if !actualCategories[category] {
			t.Errorf("%s: %s missing required plural category %q", lang, key, category)
		}
	}
	for category := range actualCategories {
		if !containsString(wanted, category) && (category != "zero" || !optionalZero) {
			t.Errorf("%s: %s has unsupported plural category %q", lang, key, category)
		}
	}
	for _, category := range baseCategoriesList(baseCategories) {
		baseValue := english[key+"."+category]
		baseOther := withoutCount(interpolationPattern.FindAllString(baseValue, -1))
		for actual := range actualCategories {
			value := locale[key+"."+actual]
			if got := withoutCount(interpolationPattern.FindAllString(value, -1)); !sameStringSlice(baseOther, got) {
				t.Errorf("%s: %s.%s has inconsistent non-count placeholders", lang, key, actual)
			}
		}
	}
}

func pluralCategories(values map[string]string, key string) map[string]bool {
	categories := map[string]bool{}
	for _, category := range []string{"zero", "one", "two", "few", "many", "other"} {
		if _, ok := values[key+"."+category]; ok {
			categories[category] = true
		}
	}
	return categories
}

func baseCategoriesList(categories map[string]bool) []string {
	result := make([]string, 0, len(categories))
	for category := range categories {
		result = append(result, category)
	}
	return result
}

func withoutCount(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "{count}" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func sameStringSlice(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// TestLocalesFS confirms every registered locale is embedded as valid JSON.
func TestLocalesFS(t *testing.T) {
	for _, lang := range supportedOrder {
		data, ok := LocaleJSON(lang)
		if !ok {
			t.Errorf("%s locale is not embedded", lang)
			continue
		}
		var locale map[string]interface{}
		if err := json.Unmarshal(data, &locale); err != nil {
			t.Errorf("%s locale is invalid JSON: %v", lang, err)
		}
	}
}

func samePlaceholders(first, second string) bool {
	left := interpolationPattern.FindAllString(first, -1)
	right := interpolationPattern.FindAllString(second, -1)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// TestSupportedLanguages verifies all seven mandated languages compile in and
// English is always present (AI.md PART 30 "Supported Languages").
func TestSupportedLanguages(t *testing.T) {
	for _, lang := range []string{"en", "es", "zh", "fr", "ar", "de", "it", "ja"} {
		if !IsSupported(lang) {
			t.Errorf("expected %q to be supported", lang)
		}
	}
	if IsSupported("xx") {
		t.Error("expected xx to be unsupported")
	}
	if !IsSupported("EN") {
		t.Error("expected case-insensitive support for EN")
	}
}

func TestLanguagePreferenceCookieLifecycle(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := LanguagePreference(request); got != "auto" {
		t.Fatalf("default preference = %q, want auto", got)
	}
	writer := httptest.NewRecorder()
	if !SetLanguageCookie(writer, request, "it") {
		t.Fatal("SetLanguageCookie rejected a supported locale")
	}
	cookies := writer.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "it" || cookies[0].HttpOnly {
		t.Fatalf("language cookie = %#v", cookies)
	}
	request.AddCookie(cookies[0])
	if got := LanguagePreference(request); got != "it" {
		t.Fatalf("saved preference = %q, want it", got)
	}
	if SetLanguageCookie(httptest.NewRecorder(), request, "xx") {
		t.Fatal("SetLanguageCookie accepted an unsupported locale")
	}
	writer = httptest.NewRecorder()
	ClearLanguageCookie(writer, request)
	cleared := writer.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("cleared language cookie = %#v", cleared)
	}
}

func TestMiddlewareWithSecureCookieAttributes(t *testing.T) {
	handler := MiddlewareWithSecure(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), func(*http.Request) bool { return true })
	req := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, req)
	cookies := writer.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName || cookies[0].Value != "fr" || cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("language middleware cookie = %#v", cookies)
	}
}

func TestMiddlewarePersistsQueryLanguageOnlyWithConsent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		canPersist bool
		wantCookie bool
	}{
		{name: "without preferences consent", canPersist: false, wantCookie: false},
		{name: "with preferences consent", canPersist: true, wantCookie: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotLang := ""
			handler := MiddlewareWithSecureAndConsent(
				http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { gotLang = LangFromContext(r.Context()) }),
				func(*http.Request) bool { return true },
				func(*http.Request) bool { return tc.canPersist },
			)
			request := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, request)
			if gotLang != "fr" {
				t.Fatalf("request language = %q, want fr", gotLang)
			}
			cookies := writer.Result().Cookies()
			if tc.wantCookie {
				if len(cookies) != 1 || cookies[0].Name != "lang" || cookies[0].Value != "fr" || cookies[0].MaxAge != 365*24*60*60 {
					t.Fatalf("language preference cookie = %#v", cookies)
				}
			} else if len(cookies) != 0 {
				t.Fatalf("language cookie was set without consent: %#v", cookies)
			}
		})
	}
}

// TestKeySetsIdentical enforces the build-time invariant that every language
// carries exactly the same keys as English (AI.md PART 30).
func TestKeySetsIdentical(t *testing.T) {
	base := map[string]bool{}
	for k := range messageKeys(translations["en"]) {
		base[k] = true
	}
	if len(base) == 0 {
		t.Fatal("english locale has no keys")
	}
	for _, lang := range SupportedLanguages() {
		if lang == "en" {
			continue
		}
		got := map[string]bool{}
		for k := range messageKeys(translations[lang]) {
			got[k] = true
			if !base[k] {
				t.Errorf("%s: orphaned key %q not in en", lang, k)
			}
		}
		for k := range base {
			if !got[k] {
				t.Errorf("%s: missing key %q present in en", lang, k)
			}
		}
	}
}

// TestNoEmptyValues enforces the spec's "no empty string values" rule.
func TestNoEmptyValues(t *testing.T) {
	for _, lang := range SupportedLanguages() {
		for _, k := range Keys(lang) {
			if Translate(lang, k) == "" {
				t.Errorf("%s: empty value for key %q", lang, k)
			}
		}
	}
}

func TestTranslate(t *testing.T) {
	if got := Translate("es", "common.save"); got != "Guardar" {
		t.Errorf("es common.save = %q, want Guardar", got)
	}
	// Unsupported language falls back to English.
	if got := Translate("xx", "common.save"); got != Translate("en", "common.save") {
		t.Errorf("unsupported lang did not fall back to English: %q", got)
	}
	// Missing key returns the key itself.
	if got := Translate("en", "no.such.key"); got != "no.such.key" {
		t.Errorf("missing key = %q, want the key itself", got)
	}
}

func TestTranslateFormat(t *testing.T) {
	// Alternating key/value pairs.
	got := TranslateFormat("en", "common.page_x_of_y", "current", 2, "total", 9)
	if got != "Page 2 of 9" {
		t.Errorf("format pairs = %q, want Page 2 of 9", got)
	}
	// Single map argument.
	got = TranslateFormat("en", "common.page_x_of_y", map[string]interface{}{"current": 3, "total": 5})
	if got != "Page 3 of 5" {
		t.Errorf("format map = %q, want Page 3 of 5", got)
	}
}

func TestTranslatePlural(t *testing.T) {
	if got := TranslatePlural("en", "plurals.items", 1); got != "1 item" {
		t.Errorf("plural 1 = %q, want '1 item'", got)
	}
	if got := TranslatePlural("en", "plurals.items", 5); got != "5 items" {
		t.Errorf("plural 5 = %q, want '5 items'", got)
	}
	if got := TranslatePlural("en", "plurals.items", 0); got != "No items" {
		t.Errorf("plural 0 = %q, want 'No items'", got)
	}
}

func TestDirection(t *testing.T) {
	if Direction("ar") != "rtl" {
		t.Errorf("Arabic direction = %q, want rtl", Direction("ar"))
	}
	if Direction("en") != "ltr" {
		t.Errorf("English direction = %q, want ltr", Direction("en"))
	}
	if Direction("xx") != "ltr" {
		t.Errorf("unknown direction = %q, want ltr fallback", Direction("xx"))
	}
}

func TestLangFromRequest(t *testing.T) {
	// Query param wins.
	r := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	r.Header.Set("Accept-Language", "de")
	if got := LangFromRequest(r); got != "fr" {
		t.Errorf("query param lang = %q, want fr", got)
	}

	// Cookie used when no query param.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "de"})
	if got := LangFromRequest(r); got != "de" {
		t.Errorf("cookie lang = %q, want de", got)
	}

	// Accept-Language used when no query/cookie.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	if got := LangFromRequest(r); got != "es" {
		t.Errorf("accept-language = %q, want es", got)
	}

	// Unsupported everything falls back to English.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "xx,yy;q=0.5")
	if got := LangFromRequest(r); got != "en" {
		t.Errorf("unsupported = %q, want en", got)
	}
}

func TestParseBestMatch(t *testing.T) {
	cases := map[string]string{
		"fr-CA,fr;q=0.9,en;q=0.5": "fr",
		"en-US,en;q=0.9":          "en",
		"de;q=0.3,ja;q=0.9":       "ja",
		"xx,yy":                   "",
		"*":                       "",
	}
	for header, want := range cases {
		if got := parseBestMatch(header); got != want {
			t.Errorf("parseBestMatch(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestResolveCLILang(t *testing.T) {
	// Flag wins.
	if got := ResolveCLILang("fr", "de"); got != "fr" {
		t.Errorf("flag priority = %q, want fr", got)
	}
	// Config used when no flag.
	if got := ResolveCLILang("", "ja"); got != "ja" {
		t.Errorf("config priority = %q, want ja", got)
	}
	// Unsupported flag falls back to English.
	if got := ResolveCLILang("xx", ""); got != "en" {
		t.Errorf("unsupported flag = %q, want en", got)
	}
	// "auto" config is ignored.
	if got := ResolveCLILang("", "auto"); got == "auto" {
		t.Error("auto config should not be treated as a language")
	}
}

func TestMiddlewareSetsCookieAndContext(t *testing.T) {
	var seen string
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = LangFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
	h.ServeHTTP(rec, req)

	if seen != "de" {
		t.Errorf("context lang = %q, want de", seen)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName && c.Value == "de" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q cookie set to de", cookieName)
	}
}

func TestLocaleJSON(t *testing.T) {
	if _, ok := LocaleJSON("en"); !ok {
		t.Error("expected en locale JSON to exist")
	}
	if _, ok := LocaleJSON("xx"); ok {
		t.Error("expected xx locale JSON to be absent")
	}
}

func TestAvailableLanguagesAndRequestTranslationHelpers(t *testing.T) {
	languages := AvailableLanguages()
	if len(languages) != len(SupportedLanguages()) {
		t.Fatalf("available language metadata count = %d, want %d", len(languages), len(SupportedLanguages()))
	}
	for _, language := range languages {
		if language.Code == "" || language.Name == "" || language.NativeName == "" || language.Direction == "" {
			t.Errorf("incomplete language metadata: %+v", language)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/?lang=es", nil)
	var got, formatted string
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = T(r, "common.save")
		formatted = TF(r, "common.page_x_of_y", "current", 2, "total", 5)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got != "Guardar" || formatted != "Página 2 de 5" {
		t.Fatalf("request translations = (%q, %q)", got, formatted)
	}
}

func TestLanguageDirectionAndLocaleLookup(t *testing.T) {
	if got := Direction("ar"); got != "rtl" {
		t.Errorf("Arabic direction = %q, want rtl", got)
	}
	if got := Direction("en"); got != "ltr" {
		t.Errorf("English direction = %q, want ltr", got)
	}
	if got := Direction("unknown"); got != "ltr" {
		t.Errorf("unsupported language direction = %q, want ltr", got)
	}
	if data, ok := LocaleJSON("ar"); !ok || len(data) == 0 {
		t.Fatalf("Arabic locale JSON = %d bytes, %t", len(data), ok)
	}
	if _, ok := LocaleJSON("unknown"); ok {
		t.Fatal("unsupported locale JSON was found")
	}
}
