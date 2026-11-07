package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLocale(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
}

func TestLoadLocalesFlattensNestedKeys(t *testing.T) {
	dir := t.TempDir()
	writeLocale(t, dir, "en.json", `{"greeting":{"hello":"Hello {name}"},"count":3,"enabled":true}`)
	writeLocale(t, dir, "notjson.txt", `not a locale file`)

	locales, err := loadLocales(dir)
	if err != nil {
		t.Fatalf("loadLocales: %v", err)
	}
	if _, ok := locales["notjson"]; ok {
		t.Error("expected non-.json files to be ignored")
	}
	en, ok := locales["en"]
	if !ok {
		t.Fatal("expected en locale to be loaded")
	}
	if en["greeting.hello"] != "Hello {name}" {
		t.Errorf("greeting.hello = %q, want %q", en["greeting.hello"], "Hello {name}")
	}
	if en["count"] != "3" {
		t.Errorf("count = %q, want %q (float flattened to string)", en["count"], "3")
	}
	if en["enabled"] != "true" {
		t.Errorf("enabled = %q, want %q (bool flattened to string)", en["enabled"], "true")
	}
}

func TestLoadLocalesMissingDirReturnsError(t *testing.T) {
	if _, err := loadLocales(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("loadLocales(missing dir) = nil error, want error")
	}
}

func TestLoadLocalesInvalidJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeLocale(t, dir, "en.json", `{not valid json`)
	if _, err := loadLocales(dir); err == nil {
		t.Error("loadLocales(invalid JSON) = nil error, want error")
	}
}

// TestCompareToBaseDetectsMissingOrphanedAndVarMismatch exercises a target
// locale where "a" matches, "b" is missing entirely, "c" has a var mismatch
// ({n} vs {count}), and "d" is an orphan not present in base.
func TestCompareToBaseDetectsMissingOrphanedAndVarMismatch(t *testing.T) {
	base := map[string]string{
		"a": "Hello {name}",
		"b": "World",
		"c": "Count is {n}",
	}
	target := map[string]string{
		"a": "Bonjour {name}",
		"c": "Le compte est {count}",
		"d": "orphan value",
	}

	problems := compareToBase("fr", base, target)

	var hasMissing, hasOrphan, hasVarMismatch bool
	for _, p := range problems {
		switch {
		case containsAll(p, "fr", "missing key", `"b"`):
			hasMissing = true
		case containsAll(p, "fr", "orphaned key", `"d"`):
			hasOrphan = true
		case containsAll(p, "fr", "interpolation vars", `"c"`):
			hasVarMismatch = true
		}
	}
	if !hasMissing {
		t.Errorf("expected a missing-key problem for %q, got: %v", "b", problems)
	}
	if !hasOrphan {
		t.Errorf("expected an orphaned-key problem for %q, got: %v", "d", problems)
	}
	if !hasVarMismatch {
		t.Errorf("expected an interpolation-var mismatch for %q, got: %v", "c", problems)
	}
	if len(problems) != 3 {
		t.Errorf("got %d problems, want 3: %v", len(problems), problems)
	}
}

func TestCompareToBaseIdenticalLocalesHasNoProblems(t *testing.T) {
	base := map[string]string{"a": "Hello {name}", "b": "World"}
	target := map[string]string{"a": "Bonjour {name}", "b": "Monde"}
	if problems := compareToBase("fr", base, target); len(problems) != 0 {
		t.Errorf("expected no problems for a structurally identical locale, got: %v", problems)
	}
}

func TestCheckValuesDetectsEmptyAndWhitespaceOnly(t *testing.T) {
	target := map[string]string{
		"a": "value",
		"b": "",
		"c": "   ",
	}
	problems := checkValues("en", target)
	if len(problems) != 2 {
		t.Errorf("got %d problems, want 2 (empty and whitespace-only): %v", len(problems), problems)
	}
}

func TestVarSet(t *testing.T) {
	got := varSet("Hello {name}, you have {count} items and {count} more")
	want := map[string]bool{"{name}": true, "{count}": true}
	if !equalSets(got, want) {
		t.Errorf("varSet() = %v, want %v", got, want)
	}
	if empty := varSet("no vars here"); len(empty) != 0 {
		t.Errorf("varSet(no vars) = %v, want empty", empty)
	}
}

func TestEqualSets(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]bool
		want bool
	}{
		{"both empty", map[string]bool{}, map[string]bool{}, true},
		{"same contents", map[string]bool{"x": true, "y": true}, map[string]bool{"y": true, "x": true}, true},
		{"different lengths", map[string]bool{"x": true}, map[string]bool{"x": true, "y": true}, false},
		{"same length different keys", map[string]bool{"x": true}, map[string]bool{"y": true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := equalSets(tc.a, tc.b); got != tc.want {
				t.Errorf("equalSets(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSortedSetAndSortedKeys(t *testing.T) {
	set := map[string]bool{"{b}": true, "{a}": true, "{c}": true}
	got := sortedSet(set)
	want := []string{"{a}", "{b}", "{c}"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("sortedSet() = %v, want %v", got, want)
	}

	m := map[string]map[string]string{"fr": nil, "en": nil, "de": nil}
	gotKeys := sortedKeys(m)
	wantKeys := []string{"de", "en", "fr"}
	if !stringSlicesEqual(gotKeys, wantKeys) {
		t.Errorf("sortedKeys() = %v, want %v", gotKeys, wantKeys)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !stringContains(s, sub) {
			return false
		}
	}
	return true
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return len(substr) == 0
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
