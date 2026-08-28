package tui

import "strings"

// MarkdownSource is the immutable source owned by a completed transcript cell.
// CWD is captured with the message so a later attachment cannot change link meaning.
type MarkdownSource struct {
	Text string
	CWD  string
}

func newMarkdownSource(text, cwd string) MarkdownSource {
	return MarkdownSource{Text: text, CWD: cwd}
}

// markdownParseSource supplies a terminal newline only to the parser. The
// synthetic byte never becomes part of MarkdownSource or a copied transcript.
func markdownParseSource(source string) string {
	if source != "" && !strings.HasSuffix(source, "\n") {
		return source + "\n"
	}
	return source
}
