package unit

import (
	"testing"

	"github.com/apimgr/gitignore/src/templates"
)

func TestTemplateManager_LoadTemplates(t *testing.T) {
	tm := templates.NewTemplateManager()
	err := tm.LoadTemplates()
	if err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	count := tm.Count()
	if count == 0 {
		t.Error("No templates loaded")
	}
	t.Logf("Loaded %d templates", count)
}

func TestTemplateManager_GetTemplate(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{"Go template exists", "Go", false},
		{"Python template exists", "Python", false},
		{"Node template exists", "Node", false},
		{"Invalid template", "NonExistent", true},
		{"Empty name", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := tm.GetTemplate(tt.template)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(content) == 0 {
					t.Error("Template content is empty")
				}
			}
		})
	}
}

func TestTemplateManager_Search(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantMin int
	}{
		{"Search Go", "go", 1},
		{"Search Python", "python", 1},
		{"Search case insensitive", "GO", 1},
		{"Search empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := tm.Search(tt.query)
			if len(results) < tt.wantMin {
				t.Errorf("Expected at least %d results, got %d", tt.wantMin, len(results))
			}
		})
	}
}

func TestTemplateManager_List(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	list := tm.List()
	if len(list) == 0 {
		t.Error("Template list is empty")
	}

	// Check that list is sorted
	for i := 1; i < len(list); i++ {
		if list[i-1] >= list[i] {
			t.Errorf("List not sorted: %s >= %s", list[i-1], list[i])
		}
	}
}

func TestTemplateManager_GetCategories(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	categories := tm.GetCategories()
	if len(categories) == 0 {
		t.Error("No categories found")
	}

	expectedCategories := []string{"Global", "Languages"}
	for _, expected := range expectedCategories {
		found := false
		for _, category := range categories {
			if category == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected category %s not found", expected)
		}
	}
}

func TestTemplateManager_GetByCategory(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	templates := tm.GetByCategory("Languages")
	if len(templates) == 0 {
		t.Error("No templates in Languages category")
	}
}

func TestTemplateManager_CombineTemplates(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	tests := []struct {
		name      string
		templates []string
		wantErr   bool
	}{
		{"Combine two templates", []string{"Go", "Python"}, false},
		{"Combine with invalid", []string{"Go", "Invalid"}, true},
		{"Empty list", []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := tm.CombineTemplates(tt.templates)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(content) == 0 {
					t.Error("Combined content is empty")
				}
			}
		})
	}
}

func TestTemplateManager_Deduplication(t *testing.T) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		t.Fatalf("Failed to load templates: %v", err)
	}

	// Combine same template twice
	content, err := tm.CombineTemplates([]string{"Go", "Go"})
	if err != nil {
		t.Fatalf("Failed to combine templates: %v", err)
	}

	// Get single template for comparison
	single, err := tm.GetTemplate("Go")
	if err != nil {
		t.Fatalf("Failed to get template: %v", err)
	}

	// Combined content should not be twice as long
	// (allowing some overhead for headers)
	if len(content) > len(single)*2 {
		t.Error("Deduplication not working: content too large")
	}
}

func BenchmarkTemplateManager_LoadTemplates(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tm := templates.NewTemplateManager()
		if err := tm.LoadTemplates(); err != nil {
			b.Fatalf("Failed to load templates: %v", err)
		}
	}
}

func BenchmarkTemplateManager_GetTemplate(b *testing.B) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		b.Fatalf("Failed to load templates: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tm.GetTemplate("Go")
		if err != nil {
			b.Fatalf("Failed to get template: %v", err)
		}
	}
}

func BenchmarkTemplateManager_Search(b *testing.B) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		b.Fatalf("Failed to load templates: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tm.Search("python")
	}
}

func BenchmarkTemplateManager_CombineTemplates(b *testing.B) {
	tm := templates.NewTemplateManager()
	if err := tm.LoadTemplates(); err != nil {
		b.Fatalf("Failed to load templates: %v", err)
	}

	templates := []string{"Go", "Python", "Node"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tm.CombineTemplates(templates)
		if err != nil {
			b.Fatalf("Failed to combine templates: %v", err)
		}
	}
}
