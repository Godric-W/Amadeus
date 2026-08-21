package webfetch

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

var markdownBlankLines = regexp.MustCompile(`\n{3,}`)

func htmlToMarkdown(content string, baseURL *url.URL) (string, string, error) {
	document, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", "", err
	}
	title := strings.TrimSpace(collapsedText(findElement(document, "title")))
	root := readableRoot(document)
	renderer := markdownRenderer{baseURL: baseURL}
	markdown := renderer.renderChildren(root)
	markdown = normalizeMarkdown(markdown)
	return title, markdown, nil
}

type markdownRenderer struct {
	baseURL *url.URL
}

func (renderer markdownRenderer) render(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.TextNode {
		return inlineText(node.Data)
	}
	if node.Type != html.ElementNode && node.Type != html.DocumentNode {
		return renderer.renderChildren(node)
	}
	name := strings.ToLower(node.Data)
	switch name {
	case "script", "style", "nav", "footer", "header", "iframe", "noscript", "svg", "canvas", "form", "button", "aside", "template":
		return ""
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(name[1] - '0')
		return block(strings.Repeat("#", level) + " " + strings.TrimSpace(renderer.renderChildren(node)))
	case "p", "div", "section", "article", "main", "figure", "figcaption", "details", "summary":
		return block(renderer.renderChildren(node))
	case "br":
		return "\n"
	case "hr":
		return "\n\n---\n\n"
	case "strong", "b":
		return wrapInline("**", renderer.renderChildren(node))
	case "em", "i":
		return wrapInline("*", renderer.renderChildren(node))
	case "del", "s", "strike":
		return wrapInline("~~", renderer.renderChildren(node))
	case "code":
		return inlineCode(collapsedText(node))
	case "pre":
		return fencedCode(node)
	case "a":
		return renderer.renderLink(node)
	case "ul":
		return renderer.renderList(node, false)
	case "ol":
		return renderer.renderList(node, true)
	case "li":
		return renderer.renderChildren(node)
	case "blockquote":
		value := normalizeMarkdown(renderer.renderChildren(node))
		if value == "" {
			return ""
		}
		lines := strings.Split(value, "\n")
		for index := range lines {
			lines[index] = "> " + lines[index]
		}
		return block(strings.Join(lines, "\n"))
	case "img", "source", "picture", "meta", "link", "input":
		return ""
	default:
		return renderer.renderChildren(node)
	}
}

func (renderer markdownRenderer) renderChildren(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(renderer.render(child))
	}
	return builder.String()
}

func (renderer markdownRenderer) renderLink(node *html.Node) string {
	label := strings.TrimSpace(renderer.renderChildren(node))
	if label == "" {
		return ""
	}
	href := strings.TrimSpace(attribute(node, "href"))
	if href == "" {
		return label
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return label
	}
	if renderer.baseURL != nil {
		parsed = renderer.baseURL.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return label
	}
	parsed.Fragment = ""
	return "[" + label + "](" + strings.ReplaceAll(parsed.String(), ")", "%29") + ")"
}

func (renderer markdownRenderer) renderList(node *html.Node, ordered bool) string {
	items := make([]string, 0)
	itemNumber := 1
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || strings.ToLower(child.Data) != "li" {
			continue
		}
		value := normalizeMarkdown(renderer.renderChildren(child))
		if value == "" {
			continue
		}
		prefix := "- "
		if ordered {
			prefix = fmt.Sprintf("%d. ", itemNumber)
			itemNumber++
		}
		lines := strings.Split(value, "\n")
		lines[0] = prefix + lines[0]
		indent := strings.Repeat(" ", len(prefix))
		for index := 1; index < len(lines); index++ {
			if strings.TrimSpace(lines[index]) != "" {
				lines[index] = indent + lines[index]
			}
		}
		items = append(items, strings.Join(lines, "\n"))
	}
	return block(strings.Join(items, "\n"))
}

func readableRoot(document *html.Node) *html.Node {
	for _, name := range []string{"main", "article", "body"} {
		if found := findElement(document, name); found != nil {
			return found
		}
	}
	return document
}

func findElement(node *html.Node, name string) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, name) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func collapsedText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}

func attribute(node *html.Node, name string) string {
	for _, current := range node.Attr {
		if strings.EqualFold(current.Key, name) {
			return current.Val
		}
	}
	return ""
}

func fencedCode(node *html.Node) string {
	content := strings.Trim(strings.ReplaceAll(rawText(node), "\r\n", "\n"), "\n")
	if content == "" {
		return ""
	}
	language := ""
	if code := findElement(node, "code"); code != nil {
		for _, class := range strings.Fields(attribute(code, "class")) {
			if strings.HasPrefix(class, "language-") {
				language = strings.TrimPrefix(class, "language-")
				break
			}
		}
	}
	fence := "```"
	if strings.Contains(content, fence) {
		fence = "````"
	}
	return "\n\n" + fence + language + "\n" + content + "\n" + fence + "\n\n"
}

func rawText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func inlineText(value string) string {
	if value == "" {
		return ""
	}
	leadingRune, _ := utf8.DecodeRuneInString(value)
	trailingRune, _ := utf8.DecodeLastRuneInString(value)
	leading := unicode.IsSpace(leadingRune)
	trailing := unicode.IsSpace(trailingRune)
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return " "
	}
	if leading {
		value = " " + value
	}
	if trailing {
		value += " "
	}
	return value
}

func inlineCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	fence := "`"
	if strings.Contains(value, "`") {
		fence = "``"
	}
	return fence + value + fence
}

func wrapInline(marker, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return marker + value + marker
}

func block(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "\n\n" + value + "\n\n"
}

func normalizeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	inFence := false
	for index := range lines {
		trimmed := strings.TrimSpace(lines[index])
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			lines[index] = trimmed
			continue
		}
		if inFence {
			lines[index] = strings.TrimRight(lines[index], " \t")
			continue
		}
		lines[index] = strings.TrimSpace(lines[index])
	}
	value = strings.Join(lines, "\n")
	value = markdownBlankLines.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}
