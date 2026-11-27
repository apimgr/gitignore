package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccentCSSDerivesAccessibleThemeColors(t *testing.T) {
	css, ok := accentCSS("#ff79c6")
	if !ok {
		t.Fatal("accentCSS rejected a valid accent")
	}
	for _, want := range []string{":root", "html.theme-light", "html.theme-auto", "--color-primary-bg", "rgba("} {
		if !strings.Contains(css, want) {
			t.Errorf("accent CSS missing %q", want)
		}
	}
	dark := contrastAccent(255, 121, 198, 40, 42, 54, true)
	light := contrastAccent(255, 121, 198, 255, 255, 255, false)
	if contrastRatio(luminance(float64(dark.r)/255, float64(dark.g)/255, float64(dark.b)/255), luminance(40.0/255, 42.0/255, 54.0/255)) < 4.5 {
		t.Errorf("dark accent contrast is below 4.5:1: %s", dark.hex)
	}
	if contrastRatio(luminance(float64(light.r)/255, float64(light.g)/255, float64(light.b)/255), 1) < 4.5 {
		t.Errorf("light accent contrast is below 4.5:1: %s", light.hex)
	}
}

func TestHandleStaticServesConfiguredAccentCSS(t *testing.T) {
	s := newTestPageServer()
	s.config.Cfg.Web.UI.Accent = "#0969da"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/css/accent.css?v=test", nil)
	s.handleStatic(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("ETag") == "" || !strings.Contains(rec.Body.String(), "--color-primary:") {
		t.Fatalf("accent asset response: status=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestAccentCSSRejectsUnsetAndInvalidValues(t *testing.T) {
	for _, value := range []string{"", "pink", "#fff", "#gg0000", "#12345678"} {
		if _, ok := accentCSS(value); ok {
			t.Errorf("accentCSS(%q) accepted invalid value", value)
		}
	}
}
