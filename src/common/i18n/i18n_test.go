package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSupportedLanguages verifies all seven mandated languages compile in and
// English is always present (AI.md PART 30 "Supported Languages").
func TestSupportedLanguages(t *testing.T) {
	for _, lang := range []string{"en", "es", "zh", "fr", "ar", "de", "ja"} {
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

// TestKeySetsIdentical enforces the build-time invariant that every language
// carries exactly the same keys as English (AI.md PART 30).
func TestKeySetsIdentical(t *testing.T) {
	base := map[string]bool{}
	for _, k := range Keys("en") {
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
		for _, k := range Keys(lang) {
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
