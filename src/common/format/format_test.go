package format

import (
	"strings"
	"testing"
	"time"
)

func TestDurationUsesLargestUnitsAndClampsNegativeValues(t *testing.T) {
	cases := []struct {
		name  string
		value time.Duration
		want  string
	}{
		{"negative", -time.Minute, "0 seconds"},
		{"seconds", 12 * time.Second, "12 seconds"},
		{"minutes and seconds", 62 * time.Second, "1 minute 2 seconds"},
		{"hours and minutes", time.Hour + 2*time.Minute, "1 hour 2 minutes"},
		{"days and hours", 25 * time.Hour, "1 day 1 hour"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Duration(tc.value, "en"); got != tc.want {
				t.Errorf("Duration(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestSizeAndCountAreLocalized(t *testing.T) {
	if got := Size(1024, "en"); got != "1 kilobyte" {
		t.Errorf("Size(1024) = %q, want %q", got, "1 kilobyte")
	}
	if got := Size(1536, "en"); got != "1.5 kilobytes" {
		t.Errorf("Size(1536) = %q, want %q", got, "1.5 kilobytes")
	}
	if got := Count(12345, "en"); got != "12,345" {
		t.Errorf("Count(12345) = %q, want %q", got, "12,345")
	}
	for _, lang := range []string{"ar", "de", "en", "es", "fr", "it", "ja", "zh"} {
		if got := Duration(90*time.Second, lang); got == "" || strings.Contains(got, "{count}") {
			t.Errorf("Duration locale %q = %q", lang, got)
		}
		if got := Size(1024, lang); got == "" || strings.Contains(got, "{count}") {
			t.Errorf("Size locale %q = %q", lang, got)
		}
	}
}
