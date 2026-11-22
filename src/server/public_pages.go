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

const privacyCollection = `**We collect only what is necessary to provide our service:**

**Usage Information (with consent):**
- Pages visited and features used
- Browser type and device information
- Approximate location (country/region from IP, not precise)

**Technical Information:**
- IP address (for security and abuse prevention)
- API token identifiers (hashed, never stored in plain text)

**We do NOT collect:**
- Payment information (unless explicitly required by the service)
- Precise location data
- Data from other websites or apps`

const privacyUsage = `**Your data is used solely to:**

- **Provide the service:** Authentication, core functionality
- **Improve the experience:** Performance optimization, bug fixes, feature improvements
- **Ensure security:** Prevent abuse, detect fraud, protect the service
- **Communicate:** Service updates, security alerts, and (with consent) product news

**Your data is NEVER:**
- Sold to third parties
- Used for targeted advertising
- Shared without your explicit consent (except as required by law)`

const privacyUsageSold = `**Your data may be used to:**

- **Provide the service:** Authentication, core functionality
- **Improve the experience:** Performance optimization, bug fixes, feature improvements
- **Ensure security:** Prevent abuse, detect fraud, protect the service
- **Communicate:** Service updates, security alerts, and (with consent) product news
- **Third-party sharing:** Your data may be shared with or sold to third parties for analytics, advertising, or other purposes as described below

**Your rights:**
- You can opt out of data sales using the CCPA control on this page
- You can delete resources with your owner token and request deletion of other data through the privacy contact
- See the Your Rights section below for details`

const privacySecurity = `**How we protect your data:**

- Data is stored on our servers (not third-party cloud services unless specified)
- API tokens are stored as SHA-256 hashes (never in plain text)
- Connections are encrypted with HTTPS/TLS
- Security audits and updates are performed regularly
- Operator actions use access controls and audit logging`

const defaultTerms = `# Terms of Service

## Acceptance
Using {app_name} means accepting these terms. Stop using the service if you do not agree.

## Acceptable use
Do not use the service unlawfully, abuse other people, bypass rate limits or access controls, or test beyond the security policy at [/server/security/policy](/server/security/policy).

## Liability
The service is provided as is, without warranty of any kind. To the extent the law allows, the operator is not liable for loss arising from its use.

## Changes
These terms may change. The current text on this page applies from the moment it is published.

## Governing law
The laws of the United States apply, without regard to conflict-of-law rules, unless the operator's own terms text names another jurisdiction.`

var inlineMarkdown = regexp.MustCompile(`\*\*(.+?)\*\*|\*(.+?)\*|` + "`" + `([^` + "`" + `]+)` + "`" + `|\[([^\]]+)\]\(([^)]+)\)`)

func privacyDataUsage(sold bool) string {
	if sold {
		return privacyUsageSold
	}
	return privacyUsage
}

func (s *Server) termsContent(r *http.Request) (string, string) {
	cfg := s.currentConfig()
	if cfg == nil {
		return strings.ReplaceAll(defaultTerms, "{app_name}", "GitIgnore API"), "markdown"
	}
	entry := cfg.Server.Branding.Content.Terms
	if source, format, ok := pageContentSource(r, entry); ok {
		return s.substitutePageVariables(source), format
	}
	return strings.ReplaceAll(defaultTerms, "{app_name}", "GitIgnore API"), "markdown"
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
	return renderPublicContent(s.substitutePageVariables(source), format)
}

func (s *Server) substitutePageVariables(source string) string {
	version, buildDate := "", ""
	if s.config != nil {
		version, buildDate = s.config.Version, humanTimestampString(s.config.BuildDate)
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

func privacyConsentMessage(p config.PrivacyConfig) string {
	if p.Data.Sold {
		return p.Consent.MessageIfSold
	}
	return p.Consent.Message
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
