package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/rivo/uniseg"
)

func TestMarkdownStreamCollectorCommitsOnlyNewlineTerminatedSource(t *testing.T) {
	collector := MarkdownStreamCollector{}
	if collector.Push("partial") {
		t.Fatal("unterminated source became renderable")
	}
	if got := collector.Source(); got != "partial" {
		t.Fatalf("raw source = %q", got)
	}
	if got := collector.CommittedSource(); got != "" {
		t.Fatalf("committed source = %q", got)
	}
	if !collector.Push(" line\n") {
		t.Fatal("newline did not advance commit watermark")
	}
	if got := collector.CommittedSource(); got != "partial line\n" {
		t.Fatalf("committed source = %q", got)
	}
	collector.Reset()
	if collector.Source() != "" || collector.CommittedSource() != "" {
		t.Fatal("reset retained attempt source")
	}
}

func TestMarkdownParseSourceDoesNotMutateRawSource(t *testing.T) {
	raw := "  indented"
	if got := markdownParseSource(raw); got != "  indented\n" {
		t.Fatalf("parse source = %q", got)
	}
	if raw != "  indented" {
		t.Fatalf("raw source mutated = %q", raw)
	}
}

func TestMarkdownWrapPreservesSpanStyleAndDestination(t *testing.T) {
	lines := wrapMarkdownLines([]MarkdownLine{{Spans: []MarkdownSpan{
		{Text: "linked-content", Style: styleAccent, Destination: "https://example.com"},
	}}}, 4)
	if len(lines) < 3 {
		t.Fatalf("wrapped lines = %#v", lines)
	}
	for _, line := range lines {
		if width := uniseg.StringWidth(markdownLineText(line)); width > 4 {
			t.Fatalf("wrapped token width = %d: %#v", width, line)
		}
		for _, span := range line.Spans {
			if span.Style != styleAccent || span.Destination != "https://example.com" {
				t.Fatalf("wrapped span lost metadata: %#v", span)
			}
		}
	}
}

func TestMarkdownWrapDoesNotSplitOrdinaryWord(t *testing.T) {
	lines := wrapMarkdownLines([]MarkdownLine{{Spans: []MarkdownSpan{{Text: "Interface overview", Style: styleBold}}}}, 9)
	if len(lines) != 2 || markdownLineText(lines[0]) != "Interface" || markdownLineText(lines[1]) != "overview" {
		t.Fatalf("word-aware wrap = %#v", lines)
	}
}

func TestMarkdownWrapPreservesSyntaxToken(t *testing.T) {
	lines := wrapMarkdownLines([]MarkdownLine{{Spans: []MarkdownSpan{{Text: "keyword", Style: styleAccent, Syntax: chroma.Keyword}}}}, 3)
	for _, line := range lines {
		for _, span := range line.Spans {
			if span.Syntax != chroma.Keyword {
				t.Fatalf("syntax token lost after wrapping: %#v", span)
			}
		}
	}
}

func TestMarkdownLinkDestinationSurvivesStructuredProjection(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("[source](https://example.com/path)\n", "/workspace"), HistoryRenderRich)
	styled := styledLinesFromMarkdown(lines)
	found := false
	for _, line := range styled {
		for _, span := range line {
			if span.Destination == "https://example.com/path" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("structured link target was lost: %#v", styled)
	}
	if len(lines) != 1 || len(lines[0].Hyperlinks) != 2 || lines[0].Hyperlinks[0].Destination != "https://example.com/path" || markdownLineText(lines[0]) != "source (https://example.com/path)" {
		t.Fatalf("typed hyperlink range was lost: %#v", lines)
	}
}

func TestMarkdownRichProjectionHandlesStrikethrough(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("~~obsolete~~\n", ""), HistoryRenderRich)
	if len(lines) != 1 || len(lines[0].Spans) != 1 || !lines[0].Spans[0].Markdown.Strikethrough {
		t.Fatalf("strikethrough projection = %#v", lines)
	}
}

func TestMarkdownInlineStylesCompose(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("[**Interface**](https://example.com)\n", ""), HistoryRenderRich)
	if len(lines) != 1 || len(lines[0].Spans) < 1 {
		t.Fatalf("inline styles = %#v", lines)
	}
	span := lines[0].Spans[0]
	if !span.Markdown.Bold || !span.Markdown.Underline || span.Destination != "https://example.com" {
		t.Fatalf("composed span = %#v", span)
	}
}

func TestMarkdownPreservesTopLevelBlockSpacing(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("# Title\n\nParagraph text.\n\n- item\n", ""), HistoryRenderRich)
	if len(lines) < 5 || len(lines[1].Spans) != 0 || len(lines[3].Spans) != 0 {
		t.Fatalf("block spacing = %#v", lines)
	}
}

func TestMarkdownLocalLinkUsesFrozenCWDForDisplay(t *testing.T) {
	lines := markdownDisplayLocalLinks([]MarkdownLine{{Spans: []MarkdownSpan{{Text: "/workspace/pkg/file.go", Destination: "/workspace/pkg/file.go"}}}}, "/workspace")
	if got := lines[0].Spans[0].Text; got != "pkg/file.go" {
		t.Fatalf("local link display = %q", got)
	}
	if got := lines[0].Spans[0].Destination; got != "/workspace/pkg/file.go" {
		t.Fatalf("local link destination = %q", got)
	}
}

func TestMarkdownLocalLinkUsesDestinationInsteadOfLabel(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("[inspect](/workspace/pkg/file.go)\n", "/workspace"), HistoryRenderRich)
	if len(lines) != 1 || markdownLineText(lines[0]) != "pkg/file.go" || len(lines[0].Hyperlinks) != 1 || lines[0].Hyperlinks[0].Destination != "/workspace/pkg/file.go" {
		t.Fatalf("local link projection = %#v", lines)
	}
}

func TestMarkdownTableFenceUnwrapIsConservative(t *testing.T) {
	table := "```markdown\n| Name | Value |\n| --- | --- |\n| A | B |\n```\n"
	if got := unwrapMarkdownTableFences(table); strings.Contains(got, "```") {
		t.Fatalf("table fence remained wrapped: %q", got)
	}
	nonTable := "```markdown\nnot a table\n```\n"
	if got := unwrapMarkdownTableFences(nonTable); got != nonTable {
		t.Fatalf("non-table fence was unwrapped: %q", got)
	}
	if got := unwrapMarkdownTableFences("```markdown\n| Name | Value |\n| --- | --- |\n"); !strings.Contains(got, "```") {
		t.Fatalf("unterminated fence was unwrapped: %q", got)
	}
	outerFour := "````markdown\n| Name | Value |\n| --- | --- |\n| A | ``` |\n````\n"
	if got := unwrapMarkdownTableFences(outerFour); strings.Contains(got, "````") || !strings.Contains(got, "| A | ``` |") {
		t.Fatalf("four-marker table fence projection = %q", got)
	}
}

func TestMarkdownTableUsesHeaderRuleAndColumns(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("| Name | Value |\n| --- | --- |\n| A | B |\n", ""), HistoryRenderRich)
	if len(lines) != 3 {
		t.Fatalf("table lines = %#v", lines)
	}
	if header := markdownLineText(lines[0]); !strings.Contains(header, "Name │ Value") {
		t.Fatalf("table header = %q", header)
	}
	if rule := markdownLineText(lines[1]); !strings.Contains(rule, "┼") {
		t.Fatalf("table rule = %q", rule)
	}
}

func TestMarkdownTableFallsBackToKeyValueAtNarrowWidth(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("| Long Header | Value |\n| --- | --- |\n| a very long table value | B |\n", ""), HistoryRenderRich)
	wrapped := wrapMarkdownLines(lines, 12)
	for _, line := range wrapped {
		if strings.Contains(markdownLineText(line), "Long Header:") {
			return
		}
	}
	t.Fatalf("narrow table did not use key/value fallback: %#v", wrapped)
}

func TestMarkdownTableActuallyWrapsCellsAtShrunkWidths(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("| Name | Description |\n| --- | --- |\n| Alpha | words that must wrap across physical table rows |\n", ""), HistoryRenderRich)
	wrapped := wrapMarkdownLines(lines, 30)
	foundContinuation := false
	for _, line := range wrapped {
		if width := uniseg.StringWidth(markdownLineText(line)); width > 30 {
			t.Fatalf("table line width = %d: %q", width, markdownLineText(line))
		}
		if strings.Contains(markdownLineText(line), "physical") || strings.Contains(markdownLineText(line), "rows") {
			foundContinuation = true
		}
	}
	if !foundContinuation || len(wrapped) < 4 {
		t.Fatalf("table did not produce wrapped physical rows: %#v", wrapped)
	}
}

func TestMarkdownLocalLinkContractCoversFileURLWindowsAndTableCells(t *testing.T) {
	if display, ok := markdownLocalLinkDisplay("file:///workspace/pkg/file.go#L12C3", "/workspace"); !ok || display != "pkg/file.go#L12C3" {
		t.Fatalf("file URL display = %q ok=%v", display, ok)
	}
	if display, ok := markdownLocalLinkDisplay(`C:\workspace\pkg\file.go:12:3`, `C:\workspace`); !ok || display != "pkg/file.go:12:3" {
		t.Fatalf("Windows display = %q ok=%v", display, ok)
	}
	markdown := "| File |\n| --- |\n| [inspect](/workspace/pkg/file.go#L12) |\n"
	lines := wrapMarkdownLines(newMarkdownRenderer().Render(newMarkdownSource(markdown, "/workspace"), HistoryRenderRich), 40)
	if rendered := strings.Join(rawMarkdownLinesFromLines(lines), "\n"); !strings.Contains(rendered, "pkg/file.go#L12") || strings.Contains(rendered, "inspect") {
		t.Fatalf("table local link projection = %q", rendered)
	}
}

func TestMarkdownWrapPreservesListContinuationIndent(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("- item words that wrap\n", ""), HistoryRenderRich)
	wrapped := wrapMarkdownLines(lines, 12)
	if len(wrapped) < 2 {
		t.Fatalf("list did not wrap: %#v", wrapped)
	}
	if got := markdownLineText(wrapped[1]); !strings.HasPrefix(got, "  ") {
		t.Fatalf("list continuation = %q", got)
	}
}

func TestMarkdownWrappedListItemIsSeparatedFromNextSibling(t *testing.T) {
	markdown := "1. This item wraps onto another visible rendered line\n2. Next item\n"
	lines := wrapMarkdownLines(newMarkdownRenderer().Render(newMarkdownSource(markdown, ""), HistoryRenderRich), 24)
	plain := rawMarkdownLinesFromLines(lines)
	if len(plain) < 5 || plain[len(plain)-2] != "" || plain[len(plain)-1] != "2. Next item" {
		t.Fatalf("wrapped list sibling projection = %#v", plain)
	}
}

func TestMarkdownHardBreakCreatesStructuredLine(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("first  \nsecond\n", ""), HistoryRenderRich)
	if len(lines) != 2 || markdownLineText(lines[0]) != "first" || markdownLineText(lines[1]) != "second" {
		t.Fatalf("hard break lines = %#v", lines)
	}
}

func TestMarkdownSoftBreakCreatesStructuredLine(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("first\nsecond\n", ""), HistoryRenderRich)
	if len(lines) != 2 || markdownLineText(lines[0]) != "first" || markdownLineText(lines[1]) != "second" {
		t.Fatalf("soft break lines = %#v", lines)
	}
}

func TestMarkdownInlineHTMLRemainsReadable(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("Inline <sup>value</sup>.\n", ""), HistoryRenderRich)
	if len(lines) != 1 || markdownLineText(lines[0]) != "Inline <sup>value</sup>." {
		t.Fatalf("inline HTML projection = %#v", lines)
	}
}

func TestMarkdownNestedListAndBlockquoteKeepStructuredIndents(t *testing.T) {
	renderer := newMarkdownRenderer()
	list := renderer.Render(newMarkdownSource("- outer\n  - nested words that wrap\n", ""), HistoryRenderRich)
	wrapped := wrapMarkdownLines(list, 18)
	if len(wrapped) < 3 || markdownLineText(wrapped[0]) != "• outer" || !strings.HasPrefix(markdownLineText(wrapped[1]), "    • nested") || !strings.HasPrefix(markdownLineText(wrapped[2]), "      ") {
		t.Fatalf("nested list projection = %#v", wrapped)
	}
	quote := wrapMarkdownLines(renderer.Render(newMarkdownSource("> quoted words that wrap\n", ""), HistoryRenderRich), 12)
	if len(quote) < 2 || !strings.HasPrefix(markdownLineText(quote[0]), "│ ") || !strings.HasPrefix(markdownLineText(quote[1]), "│ ") {
		t.Fatalf("blockquote continuation = %#v", quote)
	}
}

func TestMarkdownLooseListAndWideGlyphWrappingRemainStructured(t *testing.T) {
	renderer := newMarkdownRenderer()
	loose := renderer.Render(newMarkdownSource("- first paragraph\n\n  second paragraph\n", ""), HistoryRenderRich)
	plain := rawMarkdownLinesFromLines(loose)
	if len(plain) < 3 || plain[0] != "• first paragraph" || plain[1] != "  " || plain[2] != "  second paragraph" {
		t.Fatalf("loose list projection = %#v", plain)
	}
	wide := wrapMarkdownLines(renderer.Render(newMarkdownSource("中文内容 emoji界 text\n", ""), HistoryRenderRich), 10)
	for _, line := range wide {
		if uniseg.StringWidth(markdownLineText(line)) > 10 {
			t.Fatalf("wide glyph line overflow = %q", markdownLineText(line))
		}
	}
}

func TestMarkdownCodeCarriesNoWrapAndIndentedCodePrefix(t *testing.T) {
	renderer := newMarkdownRenderer()
	fenced := renderer.Render(newMarkdownSource("```text\na very long code line\n```\n", ""), HistoryRenderRich)
	wrapped := wrapMarkdownLines(fenced, 8)
	if len(wrapped) != 1 || !wrapped[0].NoWrap || markdownLineText(wrapped[0]) != "a very long code line" {
		t.Fatalf("fenced code projection = %#v", wrapped)
	}
	indented := renderer.Render(newMarkdownSource("    code line\n", ""), HistoryRenderRich)
	if len(indented) != 1 || !indented[0].NoWrap || markdownLineText(indented[0]) != "    code line" {
		t.Fatalf("indented code projection = %#v", indented)
	}
}

func TestMarkdownLargeCodeFallsBackWithoutHighlighting(t *testing.T) {
	large := strings.Repeat("x", maxSyntaxLineBytes+1)
	lines := newMarkdownRenderer().Render(newMarkdownSource("```go\n"+large+"\n```\n", ""), HistoryRenderRich)
	if len(lines) != 1 || len(lines[0].Spans) != 1 || lines[0].Spans[0].Syntax != chroma.EOFType {
		t.Fatalf("large code projection = %#v", lines)
	}
}

func TestMarkdownUnknownExplicitLanguageFallsBackToPlainCode(t *testing.T) {
	lines := newMarkdownRenderer().Render(newMarkdownSource("```xyzlang\nfunc maybeLooksLikeCode() {}\n```\n", ""), HistoryRenderRich)
	for _, line := range lines {
		for _, span := range line.Spans {
			if span.Syntax != chroma.EOFType {
				t.Fatalf("unknown language was guessed instead of plain fallback: %#v", lines)
			}
		}
	}
}

func TestMarkdownPlainFallbackIsBoundedAndUTF8Safe(t *testing.T) {
	source := strings.Repeat("界", maxMarkdownFallbackBytes) + "tail"
	lines := boundedPlainMarkdownLines(source)
	if len(lines) == 0 || len(lines) > maxMarkdownFallbackLines+1 {
		t.Fatalf("fallback line count = %d", len(lines))
	}
	text := strings.Join(rawMarkdownLinesFromLines(lines), "\n")
	if !utf8.ValidString(text) || !strings.Contains(text, "[Markdown display truncated]") {
		t.Fatalf("bounded fallback = %q", text[len(text)-minInt(len(text), 80):])
	}
	if !strings.HasSuffix(source, "tail") {
		t.Fatal("fallback mutated authoritative source")
	}
}

func TestMarkdownOversizedSourceSkipsStructuredParseButPreservesSource(t *testing.T) {
	source := strings.Repeat("large output line\n", maxMarkdownRenderBytes/10)
	renderer := newMarkdownRenderer()
	analysis := renderer.Analyze(source)
	lines := renderer.Render(newMarkdownSource(source, ""), HistoryRenderRich)
	if len(lines) == 0 || len(lines) > maxMarkdownFallbackLines+1 || analysis.LastTopLevelBlockStart != 0 {
		t.Fatalf("oversized Markdown projection = lines:%d analysis:%#v", len(lines), analysis)
	}
	if !strings.Contains(strings.Join(rawMarkdownLinesFromLines(lines), "\n"), "[Markdown display truncated]") {
		t.Fatal("oversized Markdown projection omitted truncation marker")
	}
	cell := NewAgentMarkdownCell(newMarkdownSource(source, ""))
	if got := strings.Join(cell.RawLines(), "\n") + "\n"; got != source {
		t.Fatal("oversized presentation fallback changed final-cell source")
	}
}

func TestOSC8WebHyperlinkValidatesDestination(t *testing.T) {
	if got := osc8WebHyperlink("https://example.com/path", "visible"); !strings.Contains(got, "\x1b]8;;https://example.com/path\x1b\\") {
		t.Fatalf("valid web link = %q", got)
	}
	if got := osc8WebHyperlink("file:///tmp/a", "visible"); got != "visible" {
		t.Fatalf("file link rendered OSC-8: %q", got)
	}
	if got := osc8WebHyperlink("https://example.com/\x07bad", "visible"); got != "visible" {
		t.Fatalf("unsafe link rendered OSC-8: %q", got)
	}
}
