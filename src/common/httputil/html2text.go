package httputil

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// HTML2TextConverter converts rendered HTML into beautifully formatted terminal
// text for non-interactive HTTP tools (curl, wget). It is a self-contained
// converter, not a library wrapper. Non-interactive and irrelevant elements
// (forms, inputs, buttons, scripts, styles) are skipped since the output is
// read-only (AI.md PART 14 "HTML2Text Conversion Rules").
func HTML2TextConverter(source string, width int) string {
	if width <= 0 {
		width = 80
	}
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return stripTags(source)
	}
	var buf strings.Builder
	convertNode(&buf, doc, width, 0)
	return strings.TrimRight(buf.String(), "\n") + "\n"
}

// convertNode recursively converts an HTML node tree into formatted text.
func convertNode(buf *strings.Builder, n *html.Node, width, indent int) {
	switch n.Type {
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "form", "input", "button", "select", "textarea", "head":
			return
		case "h1":
			text := getTextContent(n)
			line := strings.Repeat("═", width)
			buf.WriteString(line + "\n")
			buf.WriteString(centerText(strings.ToUpper(text), width) + "\n")
			buf.WriteString(line + "\n\n")
		case "h2":
			buf.WriteString("─── " + getTextContent(n) + " ───\n\n")
		case "h3":
			buf.WriteString("► " + getTextContent(n) + "\n\n")
		case "p":
			buf.WriteString(wordWrap(getTextContent(n), width-indent) + "\n\n")
		case "ul":
			convertList(buf, n, width, indent, false)
		case "ol":
			convertList(buf, n, width, indent, true)
		case "a":
			text := getTextContent(n)
			href := getAttr(n, "href")
			if href != "" {
				buf.WriteString(text + " [" + href + "]")
			} else {
				buf.WriteString(text)
			}
		case "strong", "b":
			buf.WriteString("*" + getTextContent(n) + "*")
		case "em", "i":
			buf.WriteString("_" + getTextContent(n) + "_")
		case "code":
			buf.WriteString("`" + getTextContent(n) + "`")
		case "pre":
			for _, line := range strings.Split(getTextContent(n), "\n") {
				buf.WriteString("    " + line + "\n")
			}
			buf.WriteString("\n")
		case "table":
			convertTable(buf, n, width)
		case "hr":
			buf.WriteString(strings.Repeat("─", width) + "\n\n")
		case "blockquote":
			for _, line := range strings.Split(getTextContent(n), "\n") {
				buf.WriteString("│ " + line + "\n")
			}
			buf.WriteString("\n")
		case "br":
			buf.WriteString("\n")
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				convertNode(buf, c, width, indent)
			}
		}
	case html.TextNode:
		if text := strings.TrimSpace(n.Data); text != "" {
			buf.WriteString(text)
		}
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			convertNode(buf, c, width, indent)
		}
	}
}

// convertList renders <ul>/<ol> children as bulleted or numbered lines.
func convertList(buf *strings.Builder, n *html.Node, width, indent int, ordered bool) {
	i := 1
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "li" {
			continue
		}
		marker := "  • "
		if ordered {
			marker = fmt.Sprintf("  %d. ", i)
			i++
		}
		buf.WriteString(marker + strings.TrimSpace(getTextContent(c)) + "\n")
	}
	buf.WriteString("\n")
}

// convertTable renders a <table> as an aligned text grid separated by │.
func convertTable(buf *strings.Builder, n *html.Node, width int) {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(nd *html.Node) {
		if nd.Type == html.ElementNode && nd.Data == "tr" {
			var cells []string
			for c := nd.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, strings.TrimSpace(getTextContent(c)))
				}
			}
			rows = append(rows, cells)
			return
		}
		for c := nd.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)

	var colW []int
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(colW) {
				colW = append(colW, 0)
			}
			if len(cell) > colW[i] {
				colW[i] = len(cell)
			}
		}
	}
	for _, row := range rows {
		parts := make([]string, 0, len(row))
		for i, cell := range row {
			parts = append(parts, cell+strings.Repeat(" ", colW[i]-len(cell)))
		}
		buf.WriteString(strings.Join(parts, " │ ") + "\n")
	}
	buf.WriteString("\n")
}

// getTextContent collects the concatenated, trimmed text of a node's subtree,
// skipping script and style content.
func getTextContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(nd *html.Node) {
		if nd.Type == html.ElementNode && (nd.Data == "script" || nd.Data == "style") {
			return
		}
		if nd.Type == html.TextNode {
			sb.WriteString(nd.Data)
		}
		for c := nd.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(collapseSpaces(sb.String()))
}

// getAttr returns the value of the named attribute, or "" if absent.
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// centerText pads text with leading spaces so it is centered within width.
func centerText(text string, width int) string {
	if len(text) >= width {
		return text
	}
	pad := (width - len(text)) / 2
	return strings.Repeat(" ", pad) + text
}

// wordWrap wraps text to the given width on word boundaries.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	lineLen := 0
	for i, word := range words {
		if lineLen > 0 && lineLen+1+len(word) > width {
			b.WriteString("\n")
			lineLen = 0
		} else if i > 0 && lineLen > 0 {
			b.WriteString(" ")
			lineLen++
		}
		b.WriteString(word)
		lineLen += len(word)
	}
	return b.String()
}

// collapseSpaces collapses runs of whitespace into single spaces.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// stripTags is the fallback path when HTML cannot be parsed: it removes tags and
// returns the remaining text.
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(collapseSpaces(b.String()))
}
