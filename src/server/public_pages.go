package server

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/apimgr/gitignore/src/config"
	htmlparser "golang.org/x/net/html"
)

var inlineMarkdown = regexp.MustCompile(`\*\*(.+?)\*\*|\*(.+?)\*|` + "`" + `([^` + "`" + `]+)` + "`" + `|\[([^\]]+)\]\(([^)]+)\)`)

func privacyDataUsage(r *http.Request, sold bool) string {
	if sold {
		return i18n.T(r, "web.legal.privacy.data_usage_if_sold")
	}
	return i18n.T(r, "web.legal.privacy.data_usage")
}

func localizedPrivacyDefaults(r *http.Request, privacy config.PrivacyConfig) config.PrivacyConfig {
	defaults := config.DefaultPrivacyConfig()
	if privacy.Consent.Message == defaults.Consent.Message {
		privacy.Consent.Message = i18n.T(r, "web.legal.consent.message")
	}
	if privacy.Consent.MessageIfSold == defaults.Consent.MessageIfSold {
		privacy.Consent.MessageIfSold = i18n.T(r, "web.legal.consent.message_if_sold")
	}
	if privacy.Consent.Policy.Text == defaults.Consent.Policy.Text {
		privacy.Consent.Policy.Text = i18n.T(r, "privacy.title")
	}
	if privacy.Consent.Buttons.Accept == defaults.Consent.Buttons.Accept {
		privacy.Consent.Buttons.Accept = i18n.T(r, "cookie_consent.accept")
	}
	if privacy.Consent.Buttons.Decline == defaults.Consent.Buttons.Decline {
		privacy.Consent.Buttons.Decline = i18n.T(r, "cookie_consent.decline")
	}
	if privacy.Consent.PreferencesText == defaults.Consent.PreferencesText {
		privacy.Consent.PreferencesText = i18n.T(r, "cookie_consent.manage_preferences")
	}
	if privacy.Cookies.Essential.Description == defaults.Cookies.Essential.Description {
		privacy.Cookies.Essential.Description = i18n.T(r, "web.legal.cookies.essential_description")
	}
	if privacy.Cookies.Preferences.Description == defaults.Cookies.Preferences.Description {
		privacy.Cookies.Preferences.Description = i18n.T(r, "web.legal.cookies.preferences_description")
	}
	if privacy.Cookies.Analytics.Description == defaults.Cookies.Analytics.Description {
		privacy.Cookies.Analytics.Description = i18n.T(r, "web.legal.cookies.analytics_description")
	}
	if privacy.Cookies.Analytics.DescriptionSuffixNotSold == defaults.Cookies.Analytics.DescriptionSuffixNotSold {
		privacy.Cookies.Analytics.DescriptionSuffixNotSold = i18n.T(r, "web.legal.cookies.analytics_suffix_not_sold")
	}
	if privacy.Cookies.Analytics.DescriptionSuffixSold == defaults.Cookies.Analytics.DescriptionSuffixSold {
		privacy.Cookies.Analytics.DescriptionSuffixSold = i18n.T(r, "web.legal.cookies.analytics_suffix_sold")
	}
	if privacy.Retention.Period == defaults.Retention.Period {
		privacy.Retention.Period = i18n.T(r, "web.legal.privacy.retention_period")
	}
	privacy.Data.Sharing = append([]config.PrivacySharingConfig(nil), privacy.Data.Sharing...)
	defaultSharing := make(map[string]config.PrivacySharingConfig, len(defaults.Data.Sharing))
	for _, entry := range defaults.Data.Sharing {
		defaultSharing[entry.Condition] = entry
	}
	for i := range privacy.Data.Sharing {
		entry := &privacy.Data.Sharing[i]
		defaultEntry, ok := defaultSharing[entry.Condition]
		if !ok {
			continue
		}
		key := "web.legal.privacy.sharing."
		if entry.Condition == defaultEntry.Condition {
			entry.Condition = i18n.T(r, key+"condition."+defaultEntry.Condition)
		}
		if entry.When == defaultEntry.When {
			entry.When = i18n.T(r, key+"when."+defaultEntry.Condition)
		}
		if entry.Data == defaultEntry.Data {
			entry.Data = i18n.T(r, key+"data."+defaultEntry.Condition)
		}
	}
	return privacy
}

func (s *Server) termsContent(r *http.Request) (string, string) {
	defaultContent := i18n.TF(r, "web.legal.terms.default", "app_name", "GitIgnore API")
	cfg := s.currentConfig()
	if cfg == nil {
		return defaultContent, "markdown"
	}
	entry := cfg.Server.Branding.Content.Terms
	if source, format, ok := pageContentSource(r, entry); ok {
		return s.substitutePageVariables(r, source), format
	}
	return defaultContent, "markdown"
}

func (s *Server) operatorHTML(r *http.Request, name string) template.HTML {
	cfg := s.currentConfig()
	if cfg == nil {
		return ""
	}
	var entry config.OperatorPageContent
	switch name {
	case "privacy":
		entry = cfg.Server.Branding.Content.Privacy
	case "terms":
		entry = cfg.Server.Branding.Content.Terms
	default:
		return ""
	}
	source, format, ok := pageContentSource(r, entry)
	if !ok {
		return ""
	}
	return renderPublicContent(s.substitutePageVariables(r, source), format)
}

func (s *Server) substitutePageVariables(r *http.Request, source string) string {
	version, buildDate := "", ""
	if s.config != nil {
		version, buildDate = s.config.Version, humanTimestampString(i18n.LangFromContext(r.Context()), s.config.BuildDate)
	}
	values := map[string]string{
		"{current_year}": time.Now().UTC().Format("2006"), "{project_name}": "gitignore",
		"{project_org}": "apimgr", "{project_version}": version, "{build_datetime}": buildDate,
		"{onion_address}": "", "{i2p_address}": "",
	}
	if address, ok := s.torOnionAddress.Load().(string); ok && address != "" {
		values["{onion_address}"] = strings.TrimSuffix(address, ".onion") + ".onion"
	}
	for key, value := range values {
		source = strings.ReplaceAll(source, key, value)
	}
	return source
}

func privacyConsentMessage(r *http.Request, p config.PrivacyConfig) string {
	localized := localizedPrivacyDefaults(r, p)
	if localized.Data.Sold {
		return localized.Consent.MessageIfSold
	}
	return localized.Consent.Message
}

func trackingProviderName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "matomo":
		return "Matomo"
	case "plausible":
		return "Plausible"
	case "simpleanalytics", "simple_analytics":
		return "Simple Analytics"
	case "umami":
		return "Umami"
	default:
		return name
	}
}

func pageContentSource(r *http.Request, entry config.OperatorPageContent) (string, string, bool) {
	format := strings.ToLower(strings.TrimSpace(entry.Format))
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "markdown" && format != "html" && format != "txt" {
		return "", "", false
	}
	var body string
	switch value := entry.Body.(type) {
	case string:
		body = value
	case map[string]interface{}:
		body, _ = value[i18n.LangFromContext(r.Context())].(string)
		if body == "" {
			body, _ = value["default"].(string)
		}
	case map[string]string:
		body = value[i18n.LangFromContext(r.Context())]
		if body == "" {
			body = value["default"]
		}
	case map[interface{}]interface{}:
		body, _ = value[i18n.LangFromContext(r.Context())].(string)
		if body == "" {
			body, _ = value["default"].(string)
		}
	}
	if strings.TrimSpace(body) == "" || len(body) > 256*1024 {
		return "", "", false
	}
	formatName := map[string]string{"md": "markdown", "markdown": "markdown", "html": "html", "txt": "text"}[format]
	return body, formatName, true
}

func renderPublicContent(source, format string) template.HTML {
	var raw string
	switch format {
	case "html":
		raw = source
	case "text":
		for _, paragraph := range strings.Split(strings.TrimSpace(source), "\n\n") {
			raw += "<p>" + html.EscapeString(strings.ReplaceAll(strings.TrimSpace(paragraph), "\n", " ")) + "</p>"
		}
	default:
		raw = markdownFragment(source)
	}
	return template.HTML(sanitizePublicHTML(raw))
}

func markdownFragment(source string) string {
	var out strings.Builder
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		level := 0
		for level < len(line) && level < 6 && line[level] == '#' {
			level++
		}
		if level > 0 && len(line) > level && line[level] == ' ' {
			fmt.Fprintf(&out, "<h%d>%s</h%d>", level, inlineMarkdownHTML(line[level+1:]), level)
		} else if strings.HasPrefix(line, "- ") {
			out.WriteString("<p>• " + inlineMarkdownHTML(line[2:]) + "</p>")
		} else {
			out.WriteString("<p>" + inlineMarkdownHTML(line) + "</p>")
		}
	}
	return out.String()
}

func inlineMarkdownHTML(line string) string {
	var out strings.Builder
	last := 0
	for _, match := range inlineMarkdown.FindAllStringSubmatchIndex(line, -1) {
		out.WriteString(html.EscapeString(line[last:match[0]]))
		switch {
		case match[2] >= 0:
			out.WriteString("<strong>" + html.EscapeString(line[match[2]:match[3]]) + "</strong>")
		case match[4] >= 0:
			out.WriteString("<em>" + html.EscapeString(line[match[4]:match[5]]) + "</em>")
		case match[6] >= 0:
			out.WriteString("<code>" + html.EscapeString(line[match[6]:match[7]]) + "</code>")
		case match[8] >= 0:
			label, href := line[match[8]:match[9]], line[match[10]:match[11]]
			out.WriteString("<a href=\"" + html.EscapeString(href) + "\">" + html.EscapeString(label) + "</a>")
		}
		last = match[1]
	}
	out.WriteString(html.EscapeString(line[last:]))
	return out.String()
}

func sanitizePublicHTML(source string) string {
	root, err := htmlparser.Parse(strings.NewReader("<div>" + source + "</div>"))
	if err != nil {
		return html.EscapeString(source)
	}
	var wrapper *htmlparser.Node
	var find func(*htmlparser.Node)
	find = func(n *htmlparser.Node) {
		if n.Type == htmlparser.ElementNode && n.Data == "div" && wrapper == nil {
			wrapper = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if wrapper == nil {
		return ""
	}
	var out bytes.Buffer
	var write func(*htmlparser.Node)
	write = func(n *htmlparser.Node) {
		if n.Type == htmlparser.TextNode {
			out.WriteString(html.EscapeString(n.Data))
			return
		}
		if n.Type != htmlparser.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				write(c)
			}
			return
		}
		allowed := map[string]bool{"p": true, "div": true, "span": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "em": true, "strong": true, "b": true, "i": true, "ul": true, "ol": true, "li": true, "a": true, "table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true, "pre": true, "code": true, "blockquote": true, "hr": true, "dl": true, "dt": true, "dd": true, "sup": true, "sub": true, "br": true}
		if !allowed[n.Data] {
			if n.Data == "script" || n.Data == "style" || n.Data == "form" || n.Data == "iframe" || n.Data == "object" || n.Data == "embed" {
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				write(c)
			}
			return
		}
		out.WriteByte('<')
		out.WriteString(n.Data)
		if n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" && !strings.Contains(strings.ToLower(a.Val), "javascript:") && !strings.HasPrefix(a.Val, "//") && !strings.HasPrefix(a.Val, "/server/contact") {
					out.WriteString(` href="` + html.EscapeString(a.Val) + `"`)
				}
			}
		}
		out.WriteByte('>')
		if n.Data != "hr" && n.Data != "br" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				write(c)
			}
			out.WriteString("</" + n.Data + ">")
		}
	}
	for c := wrapper.FirstChild; c != nil; c = c.NextSibling {
		write(c)
	}
	return out.String()
}
