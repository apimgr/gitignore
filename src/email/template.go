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

	"github.com/apimgr/gitignore/src/common/i18n"
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
	"startup",
	"shutdown",
	"disk_low",
	"database_error",
	"update_available",
	"update_installed",
	"update_failed",
	"update_staged",
	"test",
}

// KnownVariables is the full set of variables a template may reference: the
// global variables plus every template-specific variable (AI.md PART 17).
// Validation flags any {variable} outside this set.
var KnownVariables = map[string]bool{
	// Global variables.
	"app_name":              true,
	"app_url":               true,
	"path":                  true,
	"used_percent":          true,
	"free":                  true,
	"threshold_percent":     true,
	"fqdn":                  true,
	"onion_url":             true,
	"onion_address":         true,
	"i2p_url":               true,
	"i2p_address":           true,
	"notification_reply_to": true,
	"timestamp":             true,
	"year":                  true,
	// Template-specific variables.
	"event":           true,
	"ip":              true,
	"details":         true,
	"filename":        true,
	"size":            true,
	"error":           true,
	"expires_in":      true,
	"expiry_date":     true,
	"valid_until":     true,
	"next_retry":      true,
	"task_name":       true,
	"next_run":        true,
	"current_version": true,
	"new_version":     true,
	"reason":          true,
	"hostname":        true,
	"version":         true,
	"mode":            true,
	"since":           true,
	// Pretranslated text passed into the plain-substitution email templates.
	"email_subject":                true,
	"email_heading":                true,
	"email_from_label":             true,
	"email_time_label":             true,
	"email_since_label":            true,
	"email_mode_label":             true,
	"email_host_label":             true,
	"email_server_started":         true,
	"email_server_stopped":         true,
	"email_details_label":          true,
	"email_source_ip_label":        true,
	"email_summary":                true,
	"email_filename_label":         true,
	"email_size_label":             true,
	"email_error_label":            true,
	"email_path_full":              true,
	"email_free_space_label":       true,
	"email_threshold_label":        true,
	"email_task_failed":            true,
	"email_next_run_label":         true,
	"email_expiry_warning":         true,
	"email_expiry_date_label":      true,
	"email_renewal_failed":         true,
	"email_expires_in_label":       true,
	"email_next_retry_label":       true,
	"email_renewed":                true,
	"email_valid_until_label":      true,
	"email_smtp_confirmation":      true,
	"email_notifications_enabled":  true,
	"email_release_available":      true,
	"email_running_version_label":  true,
	"email_new_version_label":      true,
	"email_install_prompt":         true,
	"email_updated":                true,
	"email_previous_version_label": true,
	"email_failed":                 true,
	"email_version_label":          true,
	"email_reason_label":           true,
	"email_staged":                 true,
}

// templateTranslationKeys maps placeholders for static email copy to locale keys.
// Dynamic values remain in the caller's vars map and are interpolated literally.
var templateTranslationKeys = map[string]map[string]string{
	"startup": {
		"email_subject":        "email.templates.startup.subject",
		"email_heading":        "email.templates.startup.heading",
		"email_from_label":     "email.common.from_label",
		"email_time_label":     "email.common.time_label",
		"email_server_started": "email.templates.startup.summary",
		"email_version_label":  "email.common.version_label",
		"email_mode_label":     "email.templates.startup.mode_label",
		"email_host_label":     "email.common.host_label",
	},
	"shutdown": {
		"email_subject":        "email.templates.shutdown.subject",
		"email_heading":        "email.templates.shutdown.heading",
		"email_from_label":     "email.common.from_label",
		"email_time_label":     "email.common.time_label",
		"email_server_stopped": "email.templates.shutdown.summary",
		"email_version_label":  "email.common.version_label",
		"email_reason_label":   "email.templates.shutdown.reason_label",
		"email_host_label":     "email.common.host_label",
	},
	"security_alert": {
		"email_subject":         "email.templates.security_alert.subject",
		"email_heading":         "email.templates.security_alert.heading",
		"email_from_label":      "email.common.from_label",
		"email_time_label":      "email.common.time_label",
		"email_details_label":   "email.templates.security_alert.details_label",
		"email_source_ip_label": "email.templates.security_alert.source_ip_label",
	},
	"backup_complete": {
		"email_subject":        "email.templates.backup_complete.subject",
		"email_heading":        "email.templates.backup_complete.heading",
		"email_from_label":     "email.common.from_label",
		"email_time_label":     "email.common.time_label",
		"email_summary":        "email.templates.backup_complete.summary",
		"email_filename_label": "email.templates.backup_complete.filename_label",
		"email_size_label":     "email.templates.backup_complete.size_label",
	},
	"backup_failed": {
		"email_subject":     "email.templates.backup_failed.subject",
		"email_heading":     "email.templates.backup_failed.heading",
		"email_from_label":  "email.common.from_label",
		"email_time_label":  "email.common.time_label",
		"email_summary":     "email.templates.backup_failed.summary",
		"email_error_label": "email.templates.backup_failed.error_label",
	},
	"disk_low": {
		"email_subject":          "email.templates.disk_low.subject",
		"email_heading":          "email.templates.disk_low.heading",
		"email_from_label":       "email.common.from_label",
		"email_time_label":       "email.common.time_label",
		"email_path_full":        "email.templates.disk_low.path_full",
		"email_free_space_label": "email.templates.disk_low.free_space_label",
		"email_threshold_label":  "email.templates.disk_low.threshold_label",
	},
	"database_error": {
		"email_subject":     "email.templates.database_error.subject",
		"email_heading":     "email.templates.database_error.heading",
		"email_from_label":  "email.common.from_label",
		"email_time_label":  "email.common.time_label",
		"email_summary":     "email.templates.database_error.summary",
		"email_error_label": "email.templates.database_error.error_label",
		"email_since_label": "email.templates.database_error.since_label",
	},
	"scheduler_error": {
		"email_subject":        "email.templates.scheduler_error.subject",
		"email_heading":        "email.templates.scheduler_error.heading",
		"email_from_label":     "email.common.from_label",
		"email_time_label":     "email.common.time_label",
		"email_task_failed":    "email.templates.scheduler_error.task_failed",
		"email_error_label":    "email.templates.scheduler_error.error_label",
		"email_next_run_label": "email.templates.scheduler_error.next_run_label",
	},
	"ssl_expiring": {
		"email_subject":           "email.templates.ssl_expiring.subject",
		"email_heading":           "email.templates.ssl_expiring.heading",
		"email_from_label":        "email.common.from_label",
		"email_time_label":        "email.common.time_label",
		"email_expiry_warning":    "email.templates.ssl_expiring.expiry_warning",
		"email_expiry_date_label": "email.templates.ssl_expiring.expiry_date_label",
	},
	"ssl_renewal_failed": {
		"email_subject":           "email.templates.ssl_renewal_failed.subject",
		"email_heading":           "email.templates.ssl_renewal_failed.heading",
		"email_from_label":        "email.common.from_label",
		"email_time_label":        "email.common.time_label",
		"email_renewal_failed":    "email.templates.ssl_renewal_failed.renewal_failed",
		"email_error_label":       "email.templates.ssl_renewal_failed.error_label",
		"email_expires_in_label":  "email.templates.ssl_renewal_failed.expires_in_label",
		"email_expiry_date_label": "email.templates.ssl_renewal_failed.expiry_date_label",
		"email_next_retry_label":  "email.templates.ssl_renewal_failed.next_retry_label",
	},
	"ssl_renewed": {
		"email_subject":           "email.templates.ssl_renewed.subject",
		"email_heading":           "email.templates.ssl_renewed.heading",
		"email_from_label":        "email.common.from_label",
		"email_time_label":        "email.common.time_label",
		"email_renewed":           "email.templates.ssl_renewed.renewed",
		"email_valid_until_label": "email.templates.ssl_renewed.valid_until_label",
	},
	"test": {
		"email_subject":               "email.templates.test.subject",
		"email_heading":               "email.templates.test.heading",
		"email_from_label":            "email.common.from_label",
		"email_time_label":            "email.common.time_label",
		"email_smtp_confirmation":     "email.templates.test.smtp_confirmation",
		"email_notifications_enabled": "email.templates.test.notifications_enabled",
	},
	"update_available": {
		"email_subject":               "email.templates.update_available.subject",
		"email_heading":               "email.templates.update_available.heading",
		"email_from_label":            "email.common.from_label",
		"email_time_label":            "email.common.time_label",
		"email_release_available":     "email.templates.update_available.release_available",
		"email_running_version_label": "email.templates.update_available.running_version_label",
		"email_new_version_label":     "email.templates.update_available.new_version_label",
		"email_install_prompt":        "email.templates.update_available.install_prompt",
	},
	"update_installed": {
		"email_subject":                "email.templates.update_installed.subject",
		"email_heading":                "email.templates.update_installed.heading",
		"email_from_label":             "email.common.from_label",
		"email_time_label":             "email.common.time_label",
		"email_updated":                "email.templates.update_installed.updated",
		"email_previous_version_label": "email.templates.update_installed.previous_version_label",
		"email_new_version_label":      "email.templates.update_installed.new_version_label",
	},
	"update_failed": {
		"email_subject":       "email.templates.update_failed.subject",
		"email_heading":       "email.templates.update_failed.heading",
		"email_from_label":    "email.common.from_label",
		"email_time_label":    "email.common.time_label",
		"email_failed":        "email.templates.update_failed.failed",
		"email_version_label": "email.templates.update_failed.version_label",
		"email_reason_label":  "email.templates.update_failed.reason_label",
	},
	"update_staged": {
		"email_subject":       "email.templates.update_staged.subject",
		"email_heading":       "email.templates.update_staged.heading",
		"email_from_label":    "email.common.from_label",
		"email_time_label":    "email.common.time_label",
		"email_staged":        "email.templates.update_staged.staged",
		"email_version_label": "email.templates.update_staged.version_label",
	},
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
	return r.RenderLocalized(name, i18n.DefaultLang, vars)
}

// RenderLocalized resolves all built-in translated copy before applying the
// template engine's plain variable substitution. Custom template text remains
// literal unless it uses one of the same translated variables.
func (r *Renderer) RenderLocalized(name, lang string, vars map[string]string) (subject, body string, err error) {
	renderVars := make(map[string]string, len(vars)+len(templateTranslationKeys[name]))
	for key, value := range vars {
		renderVars[key] = value
	}
	for variable, key := range templateTranslationKeys[name] {
		renderVars[variable] = i18n.TranslateFormat(lang, key, renderVars)
	}
	raw, err := r.rawTemplate(name)
	if err != nil {
		return "", "", err
	}
	subject, body, err = parseTemplate(raw)
	if err != nil {
		return "", "", fmt.Errorf("email: template %q: %w", name, err)
	}
	return substitute(subject, renderVars), substitute(body, renderVars), nil
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
		return errors.New("subject cannot be empty")
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("body cannot be empty")
	}
	for _, m := range varPattern.FindAllStringSubmatch(subject+"\n"+body, -1) {
		if !KnownVariables[m[1]] {
			return fmt.Errorf("unknown variable: {%s}", m[1])
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
