package tui

import "strings"

type PlainHistoryCell struct {
	Content string
	Style   semanticStyle
}

func NewPlainHistoryCell(content string) HistoryCell {
	return PlainHistoryCell{Content: strings.TrimSpace(content), Style: stylePlain}
}
func (cell PlainHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(sanitizeContent(cell.Content), cell.Style)
}
func (cell PlainHistoryCell) RawLines() []string    { return rawLines(cell.Content) }
func (PlainHistoryCell) IsStreamContinuation() bool { return false }

type NoticeHistoryCell struct{ Content string }

func NewNoticeHistoryCell(content string) HistoryCell {
	return NoticeHistoryCell{Content: strings.TrimSpace(content)}
}
func (cell NoticeHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(sanitizeContent(cell.Content), styleDim)
}
func (cell NoticeHistoryCell) RawLines() []string    { return rawLines(cell.Content) }
func (NoticeHistoryCell) IsStreamContinuation() bool { return false }

type InfoHistoryCell struct{ Content string }

func NewInfoHistoryCell(content string) HistoryCell {
	return InfoHistoryCell{Content: strings.TrimSpace(content)}
}
func (cell InfoHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	content := sanitizeContent(cell.Content)
	if content == "" {
		return nil
	}
	return []styledLine{{{Text: "• ", Style: styleDim}, {Text: content}}}
}
func (cell InfoHistoryCell) RawLines() []string {
	if content := strings.TrimSpace(cell.Content); content != "" {
		return []string{"• " + content}
	}
	return nil
}
func (InfoHistoryCell) IsStreamContinuation() bool { return false }

type DiagnosticHistoryCell struct{ Content string }

func NewDiagnosticHistoryCell(content string) HistoryCell {
	return DiagnosticHistoryCell{Content: strings.TrimSpace(content)}
}
func (cell DiagnosticHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return styledLinesFromText(sanitizeContent(cell.Content), styleDim)
}
func (cell DiagnosticHistoryCell) RawLines() []string    { return rawLines(cell.Content) }
func (DiagnosticHistoryCell) IsStreamContinuation() bool { return false }

type WarningHistoryCell struct{ Content string }

func NewWarningHistoryCell(content string) HistoryCell {
	return WarningHistoryCell{Content: strings.TrimSpace(content)}
}
func (cell WarningHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	if cell.Content == "" {
		return nil
	}
	return []styledLine{{{Text: "⚠ " + sanitizeContent(cell.Content), Style: styleWarning}}}
}
func (cell WarningHistoryCell) RawLines() []string {
	if cell.Content == "" {
		return nil
	}
	return []string{"⚠ " + cell.Content}
}
func (WarningHistoryCell) IsStreamContinuation() bool { return false }

type ErrorHistoryCell struct{ Message string }

func NewErrorHistoryCell(message string) HistoryCell {
	return ErrorHistoryCell{Message: strings.TrimSpace(message)}
}
func (cell ErrorHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	if strings.TrimSpace(cell.Message) == "" {
		return nil
	}
	return []styledLine{{{Text: "Error: " + sanitizeContent(cell.Message), Style: styleFailure}}}
}
func (cell ErrorHistoryCell) RawLines() []string {
	if strings.TrimSpace(cell.Message) == "" {
		return nil
	}
	return []string{"Error: " + cell.Message}
}
func (ErrorHistoryCell) IsStreamContinuation() bool { return false }
