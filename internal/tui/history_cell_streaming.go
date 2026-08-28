package tui

import "github.com/Godric-W/Amadeus/internal/protocol"

// AgentMessageCell holds an emitted stable live-stream run. It deliberately
// stores rendered lines rather than Markdown source; completion consolidates
// every contiguous run for an ItemID into one AgentMarkdownCell.
type AgentMessageCell struct {
	ItemID protocol.ItemID
	Lines  []MarkdownLine
	First  bool
	Plan   bool
}

func (cell AgentMessageCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderStreamingLines(cell.Lines, cell.First, cell.Plan, ctx.Width)
}

func (cell AgentMessageCell) RawLines() []string         { return rawMarkdownLinesFromLines(cell.Lines) }
func (cell AgentMessageCell) IsStreamContinuation() bool { return !cell.First }

// StreamingAgentTailCell is the mutable portion of an active stream. It is
// never durable and is removed before completion or retry reconciliation.
type StreamingAgentTailCell struct {
	ItemID protocol.ItemID
	Lines  []MarkdownLine
	First  bool
	Plan   bool
}

func (cell StreamingAgentTailCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderStreamingLines(cell.Lines, cell.First, cell.Plan, ctx.Width)
}

func (cell StreamingAgentTailCell) RawLines() []string         { return rawMarkdownLinesFromLines(cell.Lines) }
func (cell StreamingAgentTailCell) IsStreamContinuation() bool { return !cell.First }

func renderStreamingLines(lines []MarkdownLine, first, plan bool, width int) []styledLine {
	styled := styledLinesFromMarkdown(wrapMarkdownLines(lines, maxInt(12, width-2)))
	if len(styled) == 0 {
		return nil
	}
	prefix := "  "
	if first {
		prefix = "• "
	}
	if plan && first {
		styled = append([]styledLine{{{Text: "• ", Style: styleDim}, {Text: "Proposed Plan", Style: styleBold}}}, styled...)
		prefix = "  "
	}
	styled[0] = append(styledLine{{Text: prefix, Style: styleDim}}, styled[0]...)
	for index := 1; index < len(styled); index++ {
		styled[index] = append(styledLine{{Text: "  ", Style: styleDim}}, styled[index]...)
	}
	return styled
}

func rawMarkdownLinesFromLines(lines []MarkdownLine) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		result = append(result, markdownLineText(line))
	}
	return result
}
