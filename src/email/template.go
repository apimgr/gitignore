package email

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed templates/*.txt
var defaultTemplates embed.FS

// TemplateNames lists the built-in transactional templates (AI.md PART 17
// "Default Templates").
var TemplateNames = []string{
	"security_alert",
	"backup_complete",
	"backup_failed",
	"ssl_expiring",
	"ssl_renewed",
	"ssl_renewal_failed",
	"scheduler_error",
	"update_available",
	"update_installed",
	"test",
}

// KnownVariables is the full set of variables a template may reference: the
// global variables plus every template-specific variable (AI.md PART 17).
// Validation flags any {variable} outside this set.
var KnownVariables = map[string]bool{
	// Global variables.
	"app_name":              true,
	"app_url":               true,
	"fqdn":                  true,
	"onion_url":             true,
	"onion_address":         true,
	"i2p_url":               true,
	"i2p_address":           true,
	"notification_reply_to": true,
	"timestamp":             true,
	"year":                  true,
	// Template-specific variables.
	"event":       true,
	"ip":          true,
	"details":     true,
	"filename":    true,
	"size":        true,
	"error":       true,
	"expires_in":  true,
	"expiry_date": true,
	"valid_until": true,
	"next_retry":  true,
	"task_name":   true,
	"next_run":    true,
	"current_version": true,
	"new_version":     true,
}

// varPattern matches a {variable} token.
var varPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// Renderer loads templates from an optional custom directory, falling back to
// the embedded defaults. Custom overrides live in {config_dir}/template/email/
// and take effect immediately (no caching) per AI.md PART 17 live reload.
type Renderer struct {
	// CustomDir is the on-disk override directory. Empty disables overrides.
	CustomDir string
}

// NewRenderer returns a Renderer reading overrides from customDir (may be "").
func NewRenderer(customDir string) *Renderer {
	return &Renderer{CustomDir: customDir}
}

// rawTemplate returns the template source for name, preferring a custom override
// over the embedded default.
func (r *Renderer) rawTemplate(name string) (string, error) {
	if r.CustomDir != "" {
		p := filepath.Join(r.CustomDir, name+".txt")
		if data, err := os.ReadFile(p); err == nil {
			return string(data), nil
		}
	}
	data, err := defaultTemplates.ReadFile("templates/" + name + ".txt")
	if err != nil {
		return "", fmt.Errorf("email: unknown template %q", name)
	}
	return string(data), nil
}

// parseTemplate splits raw template source into subject and body. The first line
// must be "Subject: ...", followed by a "---" separator line, then the body.
func parseTemplate(raw string) (subject, body string, err error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Subject:") {
		return "", "", errors.New("template must start with 'Subject:'")
	}
	subject = strings.TrimSpace(strings.TrimPrefix(lines[0], "Subject:"))
	sep := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			sep = i
			break
		}
	}
	if sep == -1 {
		return "", "", errors.New("template missing '---' separator")
	}
	body = strings.Join(lines[sep+1:], "\n")
	return subject, body, nil
}

// substitute replaces every {variable} in s with its value from vars. Unknown
// variables are left untouched so a rendered message never silently loses text.
func substitute(s string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(s, func(tok string) string {
		key := tok[1 : len(tok)-1]
		if v, ok := vars[key]; ok {
			return v
		}
		return tok
	})
}

// Render resolves the template named name, substitutes vars, and returns the
// finished subject and body.
func (r *Renderer) Render(name string, vars map[string]string) (subject, body string, err error) {
	raw, err := r.rawTemplate(name)
	if err != nil {
		return "", "", err
	}
	subject, body, err = parseTemplate(raw)
	if err != nil {
		return "", "", fmt.Errorf("email: template %q: %w", name, err)
	}
	return substitute(subject, vars), substitute(body, vars), nil
}

// ValidateTemplate checks raw template source for the errors listed in AI.md
// PART 17 "Template Validation": parse errors, empty subject/body, and unknown
// variables. It returns the first problem found.
func ValidateTemplate(raw string) error {
	subject, body, err := parseTemplate(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(subject) == "" {
		return errors.New("Subject cannot be empty")
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("Body cannot be empty")
	}
	for _, m := range varPattern.FindAllStringSubmatch(subject+"\n"+body, -1) {
		if !KnownVariables[m[1]] {
			return fmt.Errorf("Unknown variable: {%s}", m[1])
		}
	}
	return nil
}

// ValidateAll validates every embedded default template. It is used by
// `email validate` and by tests to guarantee the shipped defaults are correct.
func ValidateAll(r *Renderer) error {
	names := append([]string(nil), TemplateNames...)
	sort.Strings(names)
	for _, name := range names {
		raw, err := r.rawTemplate(name)
		if err != nil {
			return err
		}
		if err := ValidateTemplate(raw); err != nil {
			return fmt.Errorf("template %q: %w", name, err)
		}
	}
	return nil
}
