package template

import (
	"strings"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestNewLoadsTemplates(t *testing.T) {
	m := newTestManager(t)
	if m.Count() == 0 {
		t.Fatal("expected at least one template to be loaded")
	}
	if len(m.List()) != m.Count() {
		t.Errorf("List() length %d != Count() %d", len(m.List()), m.Count())
	}
}

func TestGetCaseInsensitive(t *testing.T) {
	m := newTestManager(t)
	names := m.List()
	if len(names) == 0 {
		t.Fatal("no templates loaded")
	}
	name := names[0]

	tmpl, err := m.Get(strings.ToUpper(name))
	if err != nil {
		t.Fatalf("Get(%q) upper case: %v", name, err)
	}
	if !strings.EqualFold(tmpl.Name, name) {
		t.Errorf("Get returned name %q, want case-insensitive match of %q", tmpl.Name, name)
	}

	tmpl2, err := m.Get(strings.ToLower(name))
	if err != nil {
		t.Fatalf("Get(%q) lower case: %v", name, err)
	}
	if tmpl2.Name != tmpl.Name {
		t.Errorf("lower/upper lookups returned different templates: %q vs %q", tmpl2.Name, tmpl.Name)
	}
}

func TestGetNotFound(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Get("definitely-not-a-real-template-xyz"); err == nil {
		t.Error("expected error for missing template")
	}
}

func TestListAll(t *testing.T) {
	m := newTestManager(t)
	all := m.ListAll()
	if len(all) != m.Count() {
		t.Errorf("ListAll() length %d != Count() %d", len(all), m.Count())
	}
	for _, tmpl := range all {
		if tmpl.Content == "" {
			t.Errorf("template %q has empty content", tmpl.Name)
		}
		if tmpl.Size != len(tmpl.Content) {
			t.Errorf("template %q Size=%d, want len(Content)=%d", tmpl.Name, tmpl.Size, len(tmpl.Content))
		}
	}
}

func TestGetCategoriesAndGetByCategory(t *testing.T) {
	m := newTestManager(t)
	cats := m.GetCategories()
	if len(cats) == 0 {
		t.Fatal("expected at least one category")
	}
	for _, cat := range cats {
		tmpls := m.GetByCategory(cat)
		if len(tmpls) == 0 {
			t.Errorf("category %q has no templates", cat)
		}
		for _, tmpl := range tmpls {
			if tmpl.Category != cat {
				t.Errorf("template %q category = %q, want %q", tmpl.Name, tmpl.Category, cat)
			}
		}
	}
}

func TestGetByCategoryUnknown(t *testing.T) {
	m := newTestManager(t)
	if got := m.GetByCategory("NoSuchCategoryAtAll"); got != nil {
		t.Errorf("GetByCategory(unknown) = %v, want nil", got)
	}
}

func TestSearchFindsByName(t *testing.T) {
	m := newTestManager(t)
	names := m.List()
	if len(names) == 0 {
		t.Fatal("no templates loaded")
	}
	name := names[0]

	results := m.Search(strings.ToLower(name))
	found := false
	for _, r := range results {
		if r.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Search(%q) did not include template %q", name, name)
	}
}

func TestSearchNoMatches(t *testing.T) {
	m := newTestManager(t)
	results := m.Search("zzz-definitely-nothing-matches-zzz")
	if len(results) != 0 {
		t.Errorf("Search(no match) = %d results, want 0", len(results))
	}
}

func TestSearchEmptyQueryMatchesAll(t *testing.T) {
	m := newTestManager(t)
	results := m.Search("")
	if len(results) != m.Count() {
		t.Errorf("Search(\"\") = %d results, want all %d templates", len(results), m.Count())
	}
}

func TestCombineSingleTemplate(t *testing.T) {
	m := newTestManager(t)
	names := m.List()
	if len(names) == 0 {
		t.Fatal("no templates loaded")
	}
	name := names[0]

	combined, err := m.Combine([]string{name})
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if !strings.Contains(combined, "Combined .gitignore") {
		t.Error("combined output missing header")
	}
	if !strings.Contains(combined, name) {
		t.Errorf("combined output missing template name %q", name)
	}
}

func TestCombineDeduplicatesPatterns(t *testing.T) {
	m := &Manager{
		templates: map[string]*Template{
			"a": {Name: "A", Content: "*.log\nbuild/\n"},
			"b": {Name: "B", Content: "*.log\ndist/\n"},
		},
		categories: map[string][]*Template{},
	}

	combined, err := m.Combine([]string{"A", "B"})
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if strings.Count(combined, "*.log") != 1 {
		t.Errorf("expected *.log to appear once (deduplicated), got %d times:\n%s", strings.Count(combined, "*.log"), combined)
	}
	if !strings.Contains(combined, "build/") || !strings.Contains(combined, "dist/") {
		t.Errorf("combined output missing unique patterns:\n%s", combined)
	}
}

func TestCombineUnknownTemplate(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Combine([]string{"not-a-real-template"}); err == nil {
		t.Error("expected error combining unknown template")
	}
}

func TestCombineEmptyList(t *testing.T) {
	m := newTestManager(t)
	combined, err := m.Combine(nil)
	if err != nil {
		t.Fatalf("Combine(nil): %v", err)
	}
	if !strings.Contains(combined, "Combined .gitignore") {
		t.Error("expected header even for empty template list")
	}
}

func TestCount(t *testing.T) {
	m := newTestManager(t)
	if m.Count() != len(m.templates) {
		t.Errorf("Count() = %d, want %d", m.Count(), len(m.templates))
	}
}

func TestStats(t *testing.T) {
	m := newTestManager(t)
	stats := m.Stats()

	total, ok := stats["total_templates"].(int)
	if !ok || total != m.Count() {
		t.Errorf("stats total_templates = %v, want %d", stats["total_templates"], m.Count())
	}

	categories, ok := stats["categories"].(int)
	if !ok || categories != len(m.GetCategories()) {
		t.Errorf("stats categories = %v, want %d", stats["categories"], len(m.GetCategories()))
	}

	totalSize, ok := stats["total_size_bytes"].(int)
	if !ok || totalSize <= 0 {
		t.Errorf("stats total_size_bytes = %v, want positive int", stats["total_size_bytes"])
	}

	breakdown, ok := stats["category_breakdown"].(map[string]int)
	if !ok || len(breakdown) == 0 {
		t.Errorf("stats category_breakdown = %v, want non-empty map", stats["category_breakdown"])
	}
}

func TestExtractDescription(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"simple description", "# Sample gitignore\n*.log\n", "Sample gitignore"},
		{"skips created-by line", "# Created by https://example.com\n# Real description\n*.log\n", "Real description"},
		{"no description", "*.log\nbuild/\n", ""},
		{"empty content", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractDescription(tc.content); got != tc.want {
				t.Errorf("extractDescription(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestExtractTags(t *testing.T) {
	cases := []struct {
		name, category string
		wantContains   []string
	}{
		{"Go", "Root", []string{"go", "root", "golang"}},
		{"Node", "Root", []string{"node", "nodejs", "npm"}},
		{"Python", "Root", []string{"python", "py"}},
		{"CustomThing", "Global", []string{"customthing", "global"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tags := extractTags(tc.name, tc.category)
			for _, want := range tc.wantContains {
				found := false
				for _, tag := range tags {
					if tag == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("extractTags(%q, %q) = %v, missing %q", tc.name, tc.category, tags, want)
				}
			}
		})
	}
}
