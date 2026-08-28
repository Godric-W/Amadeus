package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

func wrapMarkdownLines(lines []MarkdownLine, width int) []MarkdownLine {
	if width <= 1 {
		return cloneMarkdownLines(lines)
	}
	lines = layoutMarkdownTables(lines, width)
	wrapped := make([]MarkdownLine, 0, len(lines))
	for lineIndex, line := range lines {
		if line.NoWrap {
			wrapped = append(wrapped, cloneMarkdownLines([]MarkdownLine{line})...)
			continue
		}
		if line.TableRule {
			if uniseg.StringWidth(markdownLineText(line)) <= width {
				wrapped = append(wrapped, line)
			}
			continue
		}
		if len(line.Spans) == 0 {
			wrapped = append(wrapped, line)
			continue
		}
		physical := wrapMarkdownLine(line, width)
		wrapped = append(wrapped, physical...)
		if len(physical) > 1 && lineIndex+1 < len(lines) && markdownLineStartsListItem(line) && markdownLineStartsListItem(lines[lineIndex+1]) && markdownSpansWidth(line.InitialIndent) == markdownSpansWidth(lines[lineIndex+1].InitialIndent) {
			wrapped = append(wrapped, MarkdownLine{})
		}
	}
	return rebuildMarkdownHyperlinks(wrapped)
}

func markdownLineStartsListItem(line MarkdownLine) bool {
	if line.BlockKind != markdownBlockListItem {
		return false
	}
	indent := strings.TrimLeft(markdownSpansText(line.InitialIndent), " ")
	if strings.HasPrefix(indent, "• ") {
		return true
	}
	index := 0
	for index < len(indent) && indent[index] >= '0' && indent[index] <= '9' {
		index++
	}
	return index > 0 && strings.HasPrefix(indent[index:], ". ")
}

type markdownTextRange struct {
	start      int
	end        int
	whitespace bool
}

func wrapMarkdownLine(line MarkdownLine, width int) []MarkdownLine {
	text := markdownSpansText(line.Spans)
	ranges := markdownWordRanges(text)
	if len(ranges) == 0 {
		return []MarkdownLine{line}
	}
	firstVisualLine := true
	current := newWrappedMarkdownLine(line, firstVisualLine)
	currentWidth := markdownSpansWidth(current.InitialIndent)
	hasContent := false
	var pendingSpace []MarkdownSpan
	pendingSpaceWidth := 0
	var result []MarkdownLine

	flush := func() {
		if hasContent {
			result = append(result, current)
		}
		firstVisualLine = false
		current = newWrappedMarkdownLine(line, firstVisualLine)
		currentWidth = markdownSpansWidth(current.InitialIndent)
		hasContent = false
		pendingSpace = nil
		pendingSpaceWidth = 0
	}
	appendSpans := func(spans []MarkdownSpan) {
		for _, span := range spans {
			appendMarkdownSpan(&current.Spans, span)
		}
	}

	for _, textRange := range ranges {
		spans := markdownSpanRange(line.Spans, textRange.start, textRange.end)
		piece := text[textRange.start:textRange.end]
		pieceWidth := uniseg.StringWidth(piece)
		if textRange.whitespace {
			if hasContent {
				pendingSpace = spans
				pendingSpaceWidth = pieceWidth
			}
			continue
		}

		if hasContent && currentWidth+pendingSpaceWidth+pieceWidth > width {
			flush()
		}
		available := maxInt(1, width-markdownSpansWidth(current.InitialIndent))
		if pieceWidth > available && markdownBreakableTextRange(text, textRange) {
			if hasContent {
				appendSpans(pendingSpace)
				currentWidth += pendingSpaceWidth
				flush()
			}
			for _, fragment := range markdownGraphemeRanges(piece, available) {
				fragmentSpans := markdownSpanRange(spans, fragment.start, fragment.end)
				appendSpans(fragmentSpans)
				currentWidth += uniseg.StringWidth(piece[fragment.start:fragment.end])
				hasContent = true
				if fragment.end < len(piece) {
					flush()
				}
			}
			continue
		}

		if hasContent {
			appendSpans(pendingSpace)
			currentWidth += pendingSpaceWidth
		}
		appendSpans(spans)
		currentWidth += pieceWidth
		hasContent = true
		pendingSpace = nil
		pendingSpaceWidth = 0
	}
	flush()
	return result
}

func newWrappedMarkdownLine(source MarkdownLine, first bool) MarkdownLine {
	indent := source.SubsequentIndent
	if first {
		indent = source.InitialIndent
	}
	return MarkdownLine{
		InitialIndent:    append([]MarkdownSpan(nil), indent...),
		SubsequentIndent: append([]MarkdownSpan(nil), source.SubsequentIndent...),
		BlockKind:        source.BlockKind,
		NoWrap:           source.NoWrap,
	}
}

func markdownSpansText(spans []MarkdownSpan) string {
	var builder strings.Builder
	for _, span := range spans {
		builder.WriteString(span.Text)
	}
	return builder.String()
}

func markdownSpansWidth(spans []MarkdownSpan) int {
	width := 0
	for _, span := range spans {
		width += uniseg.StringWidth(span.Text)
	}
	return width
}

func appendMarkdownSpan(spans *[]MarkdownSpan, span MarkdownSpan) {
	if span.Text == "" {
		return
	}
	if len(*spans) > 0 {
		last := &(*spans)[len(*spans)-1]
		if last.Style == span.Style && last.Markdown == span.Markdown && last.Destination == span.Destination && last.Syntax == span.Syntax {
			last.Text += span.Text
			return
		}
	}
	*spans = append(*spans, span)
}

func markdownSpanRange(spans []MarkdownSpan, start, end int) []MarkdownSpan {
	result := make([]MarkdownSpan, 0, len(spans))
	offset := 0
	for _, span := range spans {
		spanEnd := offset + len(span.Text)
		if start < spanEnd && end > offset {
			from := maxInt(start, offset) - offset
			to := minInt(end, spanEnd) - offset
			part := span
			part.Text = span.Text[from:to]
			appendMarkdownSpan(&result, part)
		}
		offset = spanEnd
		if offset >= end {
			break
		}
	}
	return result
}

func markdownWordRanges(value string) []markdownTextRange {
	var ranges []markdownTextRange
	state := -1
	offset := 0
	rest := value
	for rest != "" {
		word, nextRest, nextState := uniseg.FirstWordInString(rest, state)
		if word == "" {
			break
		}
		ranges = append(ranges, markdownWhitespaceRanges(word, offset)...)
		offset += len(word)
		rest, state = nextRest, nextState
	}
	return ranges
}

func markdownWhitespaceRanges(value string, base int) []markdownTextRange {
	if value == "" {
		return nil
	}
	var ranges []markdownTextRange
	start := 0
	firstRune, _ := utf8.DecodeRuneInString(value)
	space := unicode.IsSpace(firstRune)
	for offset, character := range value {
		currentSpace := unicode.IsSpace(character)
		if offset > start && currentSpace != space {
			ranges = append(ranges, markdownTextRange{start: base + start, end: base + offset, whitespace: space})
			start = offset
			space = currentSpace
		}
	}
	ranges = append(ranges, markdownTextRange{start: base + start, end: base + len(value), whitespace: space})
	return ranges
}

func markdownBreakableToken(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 {
		return false
	}
	if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "www.") {
		return true
	}
	if strings.ContainsAny(trimmed, "/\\?#=&%_") || strings.Count(trimmed, "-") > 0 {
		return true
	}
	if len(trimmed) >= 16 {
		for _, character := range trimmed {
			if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
				return false
			}
		}
		return true
	}
	return false
}

func markdownBreakableTextRange(text string, textRange markdownTextRange) bool {
	start, end := textRange.start, textRange.end
	for start > 0 {
		character, size := utf8.DecodeLastRuneInString(text[:start])
		if unicode.IsSpace(character) {
			break
		}
		start -= size
	}
	for end < len(text) {
		character, size := utf8.DecodeRuneInString(text[end:])
		if unicode.IsSpace(character) {
			break
		}
		end += size
	}
	return markdownBreakableToken(text[start:end])
}

func rebuildMarkdownHyperlinks(lines []MarkdownLine) []MarkdownLine {
	for index := range lines {
		line := &lines[index]
		line.Hyperlinks = nil
		column := 0
		for _, span := range line.InitialIndent {
			column += uniseg.StringWidth(span.Text)
		}
		for _, span := range line.Spans {
			width := uniseg.StringWidth(span.Text)
			if span.Destination != "" && width > 0 {
				line.Hyperlinks = append(line.Hyperlinks, HyperlinkRange{Start: column, End: column + width, Destination: span.Destination})
			}
			column += width
		}
	}
	return lines
}

func markdownGraphemeRanges(value string, width int) []markdownTextRange {
	var pieces []markdownTextRange
	start := 0
	currentWidth := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		clusterWidth := graphemes.Width()
		if currentWidth > 0 && currentWidth+clusterWidth > width {
			clusterStart, _ := graphemes.Positions()
			pieces = append(pieces, markdownTextRange{start: start, end: clusterStart})
			start = clusterStart
			currentWidth = 0
		}
		currentWidth += clusterWidth
	}
	if start < len(value) {
		pieces = append(pieces, markdownTextRange{start: start, end: len(value)})
	}
	return pieces
}

func markdownLineText(line MarkdownLine) string {
	var builder strings.Builder
	for _, span := range line.InitialIndent {
		builder.WriteString(span.Text)
	}
	for _, span := range line.Spans {
		builder.WriteString(span.Text)
	}
	return builder.String()
}
