package tui

import (
	"net/url"
	"os"
	"strings"
)

// markdownLinkDestination preserves the target as typed render metadata. It
// deliberately does not emit OSC-8: AB's terminal contract uses readable text
// first and keeps hyperlink transport as a later presentation feature.
func markdownLinkDestination(destination []byte) string {
	return strings.TrimSpace(string(destination))
}

func markdownDisplayLocalLinks(lines []MarkdownLine, cwd string) []MarkdownLine {
	cwd = strings.TrimRight(strings.TrimSpace(cwd), "/")
	for lineIndex := range lines {
		lines[lineIndex].TableCells = markdownDisplayLocalLinks(lines[lineIndex].TableCells, cwd)
		seen := make(map[string]bool)
		for spanIndex := range lines[lineIndex].Spans {
			span := &lines[lineIndex].Spans[spanIndex]
			display, local := markdownLocalLinkDisplay(span.Destination, cwd)
			if span.Destination == "" || !local {
				continue
			}
			if seen[span.Destination] {
				span.Text = ""
				continue
			}
			seen[span.Destination] = true
			span.Text = display
		}
	}
	return lines
}

func markdownLocalLinkDisplay(destination, cwd string) (string, bool) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", false
	}
	display := destination
	if strings.HasPrefix(strings.ToLower(destination), "file://") {
		parsed, err := url.Parse(destination)
		if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
			return destination, false
		}
		path, err := url.PathUnescape(parsed.Path)
		if err != nil {
			return destination, false
		}
		if host := parsed.Hostname(); host != "" && !strings.EqualFold(host, "localhost") {
			path = "//" + host + "/" + strings.TrimLeft(path, "/")
		} else if len(path) >= 3 && path[0] == '/' && isASCIILetter(path[1]) && path[2] == ':' {
			path = path[1:]
		}
		display = path
		if parsed.Fragment != "" {
			display += "#" + parsed.Fragment
		}
	} else if strings.HasPrefix(destination, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			display = strings.TrimRight(home, "/\\") + "/" + strings.TrimPrefix(destination, "~/")
		}
	} else if !strings.HasPrefix(destination, "/") &&
		!strings.HasPrefix(destination, "./") &&
		!strings.HasPrefix(destination, "../") &&
		!strings.HasPrefix(destination, `\\`) &&
		!markdownWindowsAbsolutePath(destination) {
		return destination, false
	}

	display = normalizeMarkdownLocalPath(display)
	cwd = normalizeMarkdownLocalPath(cwd)
	if cwd != "" && len(display) > len(cwd) && strings.EqualFold(display[:len(cwd)], cwd) && display[len(cwd)] == '/' {
		display = display[len(cwd)+1:]
	}
	return display, true
}

func normalizeMarkdownLocalPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if strings.HasPrefix(value, "////") {
		value = "//" + strings.TrimLeft(value, "/")
	}
	return strings.TrimRight(value, "/")
}

func markdownWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && isASCIILetter(value[0]) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func isASCIILetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func markdownWebDestination(destination string) bool {
	parsed, err := url.Parse(strings.TrimSpace(destination))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func osc8WebHyperlink(destination, text string) string {
	if strings.ContainsAny(destination, "\x1b\x07\r\n") {
		return text
	}
	parsed, err := url.Parse(destination)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return text
	}
	return "\x1b]8;;" + destination + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}
