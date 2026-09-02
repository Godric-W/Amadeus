package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const (
	maxSyntaxHighlightBytes  = 256 * 1024
	maxSyntaxHighlightLines  = 2_000
	maxSyntaxLineBytes       = 8 * 1024
	maxMarkdownRenderBytes   = 2 * 1024 * 1024
	maxMarkdownFallbackBytes = 256 * 1024
	maxMarkdownFallbackLines = 2_000
)

// MarkdownLine and MarkdownSpan are the renderer's structured output. Terminal
// styling is applied only after layout, so source is never recovered from ANSI.
type MarkdownLine struct {
	Spans            []MarkdownSpan
	InitialIndent    []MarkdownSpan
	SubsequentIndent []MarkdownSpan
	Hyperlinks       []HyperlinkRange
	BlockKind        MarkdownBlockKind
	NoWrap           bool
	// Table is set only on the source projection placeholder emitted by the
	// MarkdownWriter. Layout replaces the placeholder with physical rows.
	Table     *MarkdownTable
	TableRule bool
}

type MarkdownBlockKind uint8

const (
	markdownBlockProse MarkdownBlockKind = iota
	markdownBlockHeading
	markdownBlockListItem
	markdownBlockQuote
	markdownBlockCode
	markdownBlockTable
	markdownBlockRule
)

type HyperlinkRange struct {
	Start       int
	End         int
	Destination string
}

type MarkdownStyle struct {
	Bold          bool
	Italic        bool
	Strikethrough bool
	Underline     bool
}

func (style MarkdownStyle) Patch(next MarkdownStyle) MarkdownStyle {
	return MarkdownStyle{
		Bold:          style.Bold || next.Bold,
		Italic:        style.Italic || next.Italic,
		Strikethrough: style.Strikethrough || next.Strikethrough,
		Underline:     style.Underline || next.Underline,
	}
}

type MarkdownSpan struct {
	Text        string
	Style       semanticStyle
	Markdown    MarkdownStyle
	Destination string
	Syntax      chroma.TokenType
}

type markdownRenderer struct{ parser parser.Parser }

// MarkdownWriter owns Markdown-to-layout state for one source projection. It
// is deliberately separate from StreamCore: parsing/layout has no knowledge of
// retries, items, HistoryCells, or Bubble Tea redraws.
type MarkdownWriter struct {
	renderer     markdownRenderer
	source       []byte
	lines        []MarkdownLine
	inlineStack  []MarkdownStyle
	indentStack  []markdownIndent
	needsNewline bool
}

type markdownIndent struct {
	first      string
	subsequent string
}

func newMarkdownRenderer() markdownRenderer {
	engine := goldmark.New(goldmark.WithExtensions(extension.GFM))
	return markdownRenderer{parser: engine.Parser()}
}

func (renderer markdownRenderer) Render(source MarkdownSource, mode HistoryRenderMode) (lines []MarkdownLine) {
	defer func() {
		if recover() != nil {
			lines = boundedPlainMarkdownLines(source.Text)
		}
	}()
	if len(source.Text) > maxMarkdownRenderBytes {
		return boundedPlainMarkdownLines(source.Text)
	}
	if mode == HistoryRenderRaw {
		return rawMarkdownLines(source.Text)
	}
	input := unwrapMarkdownTableFences(markdownParseSource(sanitizeContent(source.Text)))
	if input == "" {
		return nil
	}
	bytes := []byte(input)
	document := renderer.parser.Parse(text.NewReader(bytes), parser.WithContext(parser.NewContext()))
	writer := MarkdownWriter{renderer: renderer, source: bytes, lines: make([]MarkdownLine, 0, 8)}
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		writer.writeBlock(child)
	}
	return normalizeMarkdownLines(markdownDisplayLocalLinks(writer.lines, source.CWD))
}

func boundedPlainMarkdownLines(source string) []MarkdownLine {
	source = sanitizeContent(source)
	if len(source) > maxMarkdownFallbackBytes {
		source = source[:maxMarkdownFallbackBytes]
		for !utf8.ValidString(source) && len(source) > 0 {
			source = source[:len(source)-1]
		}
		source += "\n[Markdown display truncated]"
	}
	lines := rawMarkdownLines(source)
	if len(lines) > maxMarkdownFallbackLines {
		lines = append(lines[:maxMarkdownFallbackLines], MarkdownLine{Spans: []MarkdownSpan{{Text: "[Markdown display truncated]", Style: styleDim}}})
	}
	return lines
}

func (writer *MarkdownWriter) writeBlock(node ast.Node) {
	if writer == nil {
		return
	}
	if writer.needsNewline && len(writer.lines) > 0 && len(writer.lines[len(writer.lines)-1].Spans) > 0 {
		writer.lines = append(writer.lines, MarkdownLine{})
	}
	block := writer.renderBlock(node)
	if len(block) == 0 {
		return
	}
	writer.lines = append(writer.lines, block...)
	writer.needsNewline = true
}

func (writer *MarkdownWriter) renderBlock(node ast.Node) []MarkdownLine {
	indent := writer.currentIndent()
	switch value := node.(type) {
	case *ast.Heading:
		base, headingStyle := markdownHeadingStyle(value.Level)
		lines := writer.inlineLines(value, base)
		if len(lines) > 0 {
			lines[0].Spans = append([]MarkdownSpan{{Text: strings.Repeat("#", value.Level) + " ", Style: base, Markdown: headingStyle}}, lines[0].Spans...)
		}
		for lineIndex := range lines {
			for spanIndex := range lines[lineIndex].Spans {
				lines[lineIndex].Spans[spanIndex].Markdown = lines[lineIndex].Spans[spanIndex].Markdown.Patch(headingStyle)
			}
		}
		return applyMarkdownIndent(lines, indent, markdownBlockHeading, false)
	case *ast.Paragraph:
		return applyMarkdownIndent(writer.inlineLines(value, stylePlain), indent, markdownBlockProse, false)
	case *ast.TextBlock:
		return applyMarkdownIndent(writer.inlineLines(value, stylePlain), indent, markdownBlockProse, false)
	case *ast.Blockquote:
		quoteIndent := markdownIndent{first: indent.first + "│ ", subsequent: indent.subsequent + "│ "}
		writer.indentStack = append(writer.indentStack, quoteIndent)
		var lines []MarkdownLine
		for child := value.FirstChild(); child != nil; child = child.NextSibling() {
			block := writer.renderBlock(child)
			styleMarkdownQuoteLines(block)
			lines = appendMarkdownChildBlock(lines, block, quoteIndent)
		}
		writer.indentStack = writer.indentStack[:len(writer.indentStack)-1]
		for index := range lines {
			if lines[index].BlockKind == markdownBlockProse {
				lines[index].BlockKind = markdownBlockQuote
			}
		}
		return lines
	case *ast.List:
		var lines []MarkdownLine
		index := value.Start
		for item := value.FirstChild(); item != nil; item = item.NextSibling() {
			marker := "• "
			if value.IsOrdered() {
				marker = itoa(index) + ". "
				index++
			}
			lines = append(lines, writer.renderListItem(item, indent, marker, value.IsOrdered())...)
		}
		return lines
	case markdownTableNode:
		lines := applyMarkdownIndent(writer.tableLines(value, ""), indent, markdownBlockTable, false)
		if len(lines) > 0 && lines[0].Table != nil {
			lines[0].Table.Prefix = indent.first
		}
		return lines
	case *ast.FencedCodeBlock:
		return applyMarkdownIndent(writer.renderer.codeLines(string(value.Text(writer.source)), string(value.Language(writer.source))), indent, markdownBlockCode, true)
	case *ast.CodeBlock:
		codeIndent := markdownIndent{first: indent.first + "    ", subsequent: indent.subsequent + "    "}
		return applyMarkdownIndent(writer.renderer.codeLines(string(value.Text(writer.source)), ""), codeIndent, markdownBlockCode, true)
	case *ast.ThematicBreak:
		return applyMarkdownIndent([]MarkdownLine{{Spans: []MarkdownSpan{{Text: "────────", Style: styleDim}}}}, indent, markdownBlockRule, false)
	default:
		if value := strings.TrimSpace(string(node.Text(writer.source))); value != "" {
			return applyMarkdownIndent([]MarkdownLine{{Spans: []MarkdownSpan{{Text: value, Style: stylePlain}}}}, indent, markdownBlockProse, false)
		}
		return nil
	}
}

func (writer *MarkdownWriter) currentIndent() markdownIndent {
	if len(writer.indentStack) == 0 {
		return markdownIndent{}
	}
	return writer.indentStack[len(writer.indentStack)-1]
}

func markdownHeadingStyle(level int) (semanticStyle, MarkdownStyle) {
	switch level {
	case 1:
		return styleBold, MarkdownStyle{Bold: true, Underline: true}
	case 2:
		return styleBold, MarkdownStyle{Bold: true}
	case 3:
		return styleBold, MarkdownStyle{Bold: true, Italic: true}
	default:
		return stylePlain, MarkdownStyle{Italic: true}
	}
}

func styleMarkdownQuoteLines(lines []MarkdownLine) {
	for lineIndex := range lines {
		line := &lines[lineIndex]
		for _, indents := range [][]MarkdownSpan{line.InitialIndent, line.SubsequentIndent} {
			for spanIndex := range indents {
				if strings.Contains(indents[spanIndex].Text, "│") {
					indents[spanIndex].Style = styleQuote
				}
			}
		}
		if line.BlockKind == markdownBlockCode || line.BlockKind == markdownBlockTable {
			continue
		}
		for spanIndex := range line.Spans {
			if line.Spans[spanIndex].Style == stylePlain {
				line.Spans[spanIndex].Style = styleQuote
			}
		}
	}
}

func (writer *MarkdownWriter) renderListItem(item ast.Node, outer markdownIndent, marker string, ordered bool) []MarkdownLine {
	var lines []MarkdownLine
	first := true
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		continuation := strings.Repeat(" ", uniseg.StringWidth(marker))
		childIndent := markdownIndent{first: outer.first + marker, subsequent: outer.subsequent + continuation}
		if !first {
			childIndent.first = outer.subsequent + continuation
			if _, nestedList := child.(*ast.List); nestedList {
				childIndent = markdownIndent{first: outer.subsequent + "    ", subsequent: outer.subsequent + "    "}
			}
		}
		writer.indentStack = append(writer.indentStack, childIndent)
		block := writer.renderBlock(child)
		writer.indentStack = writer.indentStack[:len(writer.indentStack)-1]
		for index := range block {
			if block[index].BlockKind == markdownBlockProse {
				block[index].BlockKind = markdownBlockListItem
			}
		}
		if first && len(block) > 0 && len(block[0].InitialIndent) > 0 {
			markerStyle := stylePlain
			if ordered {
				markerStyle = styleOrderedListMarker
			}
			block[0].InitialIndent[len(block[0].InitialIndent)-1].Style = markerStyle
		}
		_, nestedList := child.(*ast.List)
		if !first && !nestedList && len(lines) > 0 && len(block) > 0 {
			lines = append(lines, MarkdownLine{InitialIndent: markdownIndentSpans(childIndent.subsequent), SubsequentIndent: markdownIndentSpans(childIndent.subsequent), BlockKind: markdownBlockListItem})
		}
		lines = append(lines, block...)
		first = false
	}
	return lines
}

func (writer *MarkdownWriter) inlineLine(parent ast.Node, base semanticStyle) MarkdownLine {
	lines := writer.inlineLines(parent, base)
	if len(lines) == 0 {
		return MarkdownLine{}
	}
	return lines[0]
}

func (writer *MarkdownWriter) inlineLines(parent ast.Node, base semanticStyle) []MarkdownLine {
	writer.inlineStack = []MarkdownStyle{{}}
	line := MarkdownLine{}
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		writer.appendInline(&line, child, base, "")
	}
	writer.inlineStack = nil
	return splitMarkdownHardBreaks(line)
}

func (writer *MarkdownWriter) appendInline(line *MarkdownLine, node ast.Node, base semanticStyle, destination string) {
	current := MarkdownStyle{}
	if len(writer.inlineStack) > 0 {
		current = writer.inlineStack[len(writer.inlineStack)-1]
	}
	appendText := func(value string, style semanticStyle, syntax chroma.TokenType) {
		line.Spans = append(line.Spans, MarkdownSpan{Text: value, Style: style, Markdown: current, Destination: destination, Syntax: syntax})
	}
	switch value := node.(type) {
	case *ast.Text:
		appendText(string(value.Text(writer.source)), base, chroma.EOFType)
		if value.HardLineBreak() || value.SoftLineBreak() {
			line.Spans = append(line.Spans, MarkdownSpan{Text: "\n"})
		}
	case *ast.String:
		appendText(string(value.Text(writer.source)), base, chroma.EOFType)
	case *ast.CodeSpan:
		appendText(string(value.Text(writer.source)), styleAccent, chroma.EOFType)
	case *ast.Emphasis:
		next := current.Patch(MarkdownStyle{Italic: value.Level == 1, Bold: value.Level == 2})
		writer.inlineStack = append(writer.inlineStack, next)
		for child := value.FirstChild(); child != nil; child = child.NextSibling() {
			writer.appendInline(line, child, base, destination)
		}
		writer.inlineStack = writer.inlineStack[:len(writer.inlineStack)-1]
	case *extast.Strikethrough:
		writer.inlineStack = append(writer.inlineStack, current.Patch(MarkdownStyle{Strikethrough: true}))
		for child := value.FirstChild(); child != nil; child = child.NextSibling() {
			writer.appendInline(line, child, base, destination)
		}
		writer.inlineStack = writer.inlineStack[:len(writer.inlineStack)-1]
	case *ast.Link:
		start := len(line.Spans)
		destination := markdownLinkDestination(value.Destination)
		writer.inlineStack = append(writer.inlineStack, current.Patch(MarkdownStyle{Underline: true}))
		for child := value.FirstChild(); child != nil; child = child.NextSibling() {
			writer.appendInline(line, child, styleAccent, destination)
		}
		writer.inlineStack = writer.inlineStack[:len(writer.inlineStack)-1]
		label := markdownSpansText(line.Spans[start:])
		if markdownWebDestination(destination) && strings.TrimSpace(label) != destination {
			line.Spans = append(line.Spans,
				MarkdownSpan{Text: " (", Style: base, Markdown: current},
				MarkdownSpan{Text: destination, Style: styleAccent, Markdown: current.Patch(MarkdownStyle{Underline: true}), Destination: destination},
				MarkdownSpan{Text: ")", Style: base, Markdown: current},
			)
		}
	case *ast.AutoLink:
		writer.inlineStack = append(writer.inlineStack, current.Patch(MarkdownStyle{Underline: true}))
		line.Spans = append(line.Spans, MarkdownSpan{Text: string(value.Text(writer.source)), Style: styleAccent, Markdown: writer.inlineStack[len(writer.inlineStack)-1], Destination: markdownLinkDestination(value.URL(writer.source)), Syntax: chroma.EOFType})
		writer.inlineStack = writer.inlineStack[:len(writer.inlineStack)-1]
	case *ast.RawHTML:
		appendText(string(value.Text(writer.source)), base, chroma.EOFType)
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			writer.appendInline(line, child, base, destination)
		}
	}
}

func splitMarkdownHardBreaks(line MarkdownLine) []MarkdownLine {
	lines := []MarkdownLine{{}}
	for _, span := range line.Spans {
		if span.Text != "\n" {
			lines[len(lines)-1].Spans = append(lines[len(lines)-1].Spans, span)
			continue
		}
		lines = append(lines, MarkdownLine{})
	}
	return lines
}

func applyMarkdownIndent(lines []MarkdownLine, indent markdownIndent, kind MarkdownBlockKind, noWrap bool) []MarkdownLine {
	for index := range lines {
		lineIndent := indent.first
		if index > 0 {
			lineIndent = indent.subsequent
		}
		lines[index].InitialIndent = append(markdownIndentSpans(lineIndent), lines[index].InitialIndent...)
		lines[index].SubsequentIndent = append(markdownIndentSpans(indent.subsequent), lines[index].SubsequentIndent...)
		lines[index].BlockKind = kind
		lines[index].NoWrap = noWrap
	}
	return lines
}

func appendMarkdownChildBlock(lines, block []MarkdownLine, indent markdownIndent) []MarkdownLine {
	if len(lines) > 0 && len(block) > 0 {
		lines = append(lines, MarkdownLine{InitialIndent: markdownIndentSpans(indent.first), SubsequentIndent: markdownIndentSpans(indent.subsequent), BlockKind: markdownBlockProse})
	}
	return append(lines, block...)
}

func markdownIndentSpans(indent string) []MarkdownSpan {
	if indent == "" {
		return nil
	}
	return []MarkdownSpan{{Text: indent, Style: styleDim}}
}

func (renderer markdownRenderer) codeLines(source, language string) []MarkdownLine {
	lines := strings.Split(strings.TrimSuffix(source, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if syntaxHighlightTooLarge(source, lines) {
		return markdownPlainCodeLines(lines)
	}
	var lexer chroma.Lexer
	if language != "" {
		lexer = lexers.Get(language)
	} else {
		lexer = lexers.Analyse(source)
	}
	if lexer == nil {
		return markdownPlainCodeLines(lines)
	}
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return markdownPlainCodeLines(lines)
	}
	output := []MarkdownLine{{}}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		parts := strings.Split(token.Value, "\n")
		for index, part := range parts {
			if part != "" {
				output[len(output)-1].Spans = append(output[len(output)-1].Spans, MarkdownSpan{Text: part, Style: chromaSemanticStyle(token.Type), Syntax: token.Type})
			}
			if index < len(parts)-1 {
				output = append(output, MarkdownLine{})
			}
		}
	}
	return output
}

func syntaxHighlightTooLarge(source string, lines []string) bool {
	if len(source) > maxSyntaxHighlightBytes || len(lines) > maxSyntaxHighlightLines {
		return true
	}
	for _, line := range lines {
		if len(line) > maxSyntaxLineBytes {
			return true
		}
	}
	return false
}

func markdownPlainCodeLines(lines []string) []MarkdownLine {
	output := make([]MarkdownLine, 0, len(lines))
	for _, line := range lines {
		output = append(output, MarkdownLine{Spans: []MarkdownSpan{{Text: line, Style: stylePlain}}})
	}
	return output
}

func chromaSemanticStyle(kind chroma.TokenType) semanticStyle {
	if kind.InCategory(chroma.Keyword) || kind == chroma.NameFunction || kind == chroma.NameClass {
		return styleAccent
	}
	if kind.InSubCategory(chroma.LiteralString) || kind.InSubCategory(chroma.LiteralNumber) {
		return styleSuccess
	}
	if kind.InCategory(chroma.Comment) {
		return styleDim
	}
	return stylePlain
}

func rawMarkdownLines(source string) []MarkdownLine {
	if source == "" {
		return nil
	}
	parts := strings.Split(source, "\n")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	lines := make([]MarkdownLine, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, MarkdownLine{Spans: []MarkdownSpan{{Text: part, Style: stylePlain}}})
	}
	return lines
}

func prependMarkdownLine(prefix string, line MarkdownLine) MarkdownLine {
	if prefix == "" {
		return line
	}
	line.InitialIndent = append(markdownIndentSpans(prefix), line.InitialIndent...)
	line.SubsequentIndent = append(markdownIndentSpans(prefix), line.SubsequentIndent...)
	return line
}

func normalizeMarkdownLines(lines []MarkdownLine) []MarkdownLine {
	result := make([]MarkdownLine, 0, len(lines))
	for _, line := range lines {
		if len(line.Spans) == 0 && (len(result) == 0 || len(result[len(result)-1].Spans) == 0) {
			continue
		}
		result = append(result, line)
	}
	for len(result) > 0 && len(result[len(result)-1].Spans) == 0 {
		result = result[:len(result)-1]
	}
	return rebuildMarkdownHyperlinks(result)
}

func itoa(value int) string { return strconv.Itoa(value) }
