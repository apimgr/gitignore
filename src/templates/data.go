package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed data/gitignore/*
var templatesFS embed.FS

// Template represents a .gitignore template
type Template struct {
	Name        string   `json:"name"`
	FileName    string   `json:"file_name"`
	Category    string   `json:"category"`
	Content     string   `json:"content,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Size        int      `json:"size"`
}

// Manager manages .gitignore templates
type Manager struct {
	templates map[string]*Template // key: lowercase name
	categories map[string][]*Template
	mu        sync.RWMutex
}

// New creates a new template manager and loads all templates
func New() (*Manager, error) {
	m := &Manager{
		templates:  make(map[string]*Template),
		categories: make(map[string][]*Template),
	}

	if err := m.loadTemplates(); err != nil {
		return nil, err
	}

	return m, nil
}

// loadTemplates loads all templates from embedded filesystem
func (m *Manager) loadTemplates() error {
	return fs.WalkDir(templatesFS, "data/gitignore", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-.gitignore files
		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".gitignore") {
			return nil
		}

		// Read file content
		content, err := fs.ReadFile(templatesFS, path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		// Determine category from path
		category := "Root"
		relPath := strings.TrimPrefix(path, "data/gitignore/")
		if strings.Contains(relPath, "/") {
			parts := strings.Split(relPath, "/")
			category = parts[0]
		}

		// Template name (without .gitignore extension)
		name := strings.TrimSuffix(d.Name(), ".gitignore")

		// Create template
		tmpl := &Template{
			Name:        name,
			FileName:    d.Name(),
			Category:    category,
			Content:     string(content),
			Description: extractDescription(string(content)),
			Tags:        extractTags(name, category),
			Size:        len(content),
		}

		// Store template (case-insensitive key)
		key := strings.ToLower(name)
		m.templates[key] = tmpl

		// Add to category index
		m.categories[category] = append(m.categories[category], tmpl)

		return nil
	})
}

// extractDescription extracts description from template content
func extractDescription(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") && !strings.Contains(line, "Created by") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

// extractTags extracts searchable tags from name and category
func extractTags(name, category string) []string {
	tags := []string{strings.ToLower(name), strings.ToLower(category)}

	// Add common aliases
	aliases := map[string][]string{
		"go":         {"golang"},
		"node":       {"nodejs", "npm"},
		"python":     {"py"},
		"javascript": {"js"},
		"typescript": {"ts"},
		"visualstudio": {"vs"},
		"visualstudiocode": {"vscode"},
	}

	lowerName := strings.ToLower(name)
	if extraTags, ok := aliases[lowerName]; ok {
		tags = append(tags, extraTags...)
	}

	return tags
}

// Get retrieves a template by name (case-insensitive)
func (m *Manager) Get(name string) (*Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := strings.ToLower(name)
	tmpl, exists := m.templates[key]
	if !exists {
		return nil, fmt.Errorf("template not found: %s", name)
	}

	return tmpl, nil
}

// List returns all template names
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.templates))
	for _, tmpl := range m.templates {
		names = append(names, tmpl.Name)
	}
	return names
}

// ListAll returns all templates
func (m *Manager) ListAll() []*Template {
	m.mu.RLock()
	defer m.mu.RUnlock()

	templates := make([]*Template, 0, len(m.templates))
	for _, tmpl := range m.templates {
		templates = append(templates, tmpl)
	}
	return templates
}

// GetCategories returns all category names
func (m *Manager) GetCategories() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	categories := make([]string, 0, len(m.categories))
	for cat := range m.categories {
		categories = append(categories, cat)
	}
	return categories
}

// GetByCategory returns all templates in a category
func (m *Manager) GetByCategory(category string) []*Template {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.categories[category]
}

// Search searches templates by query (name, category, tags)
func (m *Manager) Search(query string) []*Template {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query = strings.ToLower(query)
	results := make([]*Template, 0)

	for _, tmpl := range m.templates {
		// Search in name
		if strings.Contains(strings.ToLower(tmpl.Name), query) {
			results = append(results, tmpl)
			continue
		}

		// Search in category
		if strings.Contains(strings.ToLower(tmpl.Category), query) {
			results = append(results, tmpl)
			continue
		}

		// Search in tags
		for _, tag := range tmpl.Tags {
			if strings.Contains(tag, query) {
				results = append(results, tmpl)
				break
			}
		}

		// Search in description
		if strings.Contains(strings.ToLower(tmpl.Description), query) {
			results = append(results, tmpl)
			continue
		}
	}

	return results
}

// Combine combines multiple templates into one
func (m *Manager) Combine(names []string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var combined strings.Builder
	seen := make(map[string]bool)

	// Add header
	combined.WriteString(fmt.Sprintf("# Combined .gitignore\n# Generated: %s\n# Templates: %s\n\n",
		filepath.Base(strings.Join(names, ", ")),
		strings.Join(names, ", ")))

	for _, name := range names {
		key := strings.ToLower(name)
		tmpl, exists := m.templates[key]
		if !exists {
			return "", fmt.Errorf("template not found: %s", name)
		}

		// Add template header
		combined.WriteString(fmt.Sprintf("### %s ###\n", tmpl.Name))

		// Add content with deduplication
		lines := strings.Split(tmpl.Content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)

			// Skip empty lines and comments for deduplication check
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				combined.WriteString(line + "\n")
				continue
			}

			// Deduplicate patterns
			if !seen[trimmed] {
				seen[trimmed] = true
				combined.WriteString(line + "\n")
			}
		}

		combined.WriteString("\n")
	}

	return combined.String(), nil
}

// Count returns the total number of templates
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.templates)
}

// Stats returns template statistics
func (m *Manager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	categoryCount := make(map[string]int)
	totalSize := 0

	for _, tmpl := range m.templates {
		categoryCount[tmpl.Category]++
		totalSize += tmpl.Size
	}

	return map[string]interface{}{
		"total_templates": len(m.templates),
		"categories":      len(m.categories),
		"category_breakdown": categoryCount,
		"total_size_bytes": totalSize,
	}
}
