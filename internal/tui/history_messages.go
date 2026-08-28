package tui

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	xansi "github.com/charmbracelet/x/ansi"
)

type UserMessageCell struct{ Content string }

func NewUserMessageCell(content string) HistoryCell {
	return UserMessageCell{Content: content}
}

func (cell UserMessageCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	content := sanitizeContent(cell.Content)
	if content == "" {
		return nil
	}
	wrapped := xansi.Hardwrap("› "+content, maxInt(12, ctx.Width), true)
	return styledLinesFromText(wrapped, styleUser)
}

func (cell UserMessageCell) RawLines() []string {
	if strings.TrimSpace(cell.Content) == "" {
		return nil
	}
	return strings.Split(cell.Content, "\n")
}

func (UserMessageCell) IsStreamContinuation() bool { return false }

type AgentMarkdownCell struct {
	Source MarkdownSource
	Cache  markdownRenderCache
}

func NewAgentMarkdownCell(source MarkdownSource) HistoryCell {
	return &AgentMarkdownCell{Source: source}
}

func (cell *AgentMarkdownCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	return renderMarkdownCell(cell.Source, &cell.Cache, ctx, "• ")
}

func (cell *AgentMarkdownCell) RawLines() []string {
	return rawLinesFromMarkdownSource(cell.Source.Text)
}
func (*AgentMarkdownCell) IsStreamContinuation() bool { return false }

type ProposedPlanCell struct {
	Source MarkdownSource
	Cache  markdownRenderCache
}

func NewProposedPlanCell(source MarkdownSource) HistoryCell { return &ProposedPlanCell{Source: source} }

func (cell *ProposedPlanCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	lines := []styledLine{{{Text: "• ", Style: styleDim}, {Text: "Proposed Plan", Style: styleBold}}}
	body := renderMarkdownCell(cell.Source, &cell.Cache, ctx, "  ")
	return append(lines, body...)
}

func (cell *ProposedPlanCell) RawLines() []string {
	return append([]string{"Proposed Plan"}, rawLinesFromMarkdownSource(cell.Source.Text)...)
}
func (*ProposedPlanCell) IsStreamContinuation() bool { return false }

func renderMarkdownCell(source MarkdownSource, cache *markdownRenderCache, ctx HistoryRenderContext, firstPrefix string) []styledLine {
	if source.Text == "" {
		return nil
	}
	key := MarkdownRenderKey{Width: ctx.Width, Mode: HistoryRenderRich, Palette: ctx.Palette}
	lines := cache.Render(key, func() []MarkdownLine {
		return wrapMarkdownLines(newMarkdownRenderer().Render(source, HistoryRenderRich), maxInt(12, ctx.Width-2))
	})
	styled := styledLinesFromMarkdown(lines)
	if len(styled) == 0 {
		return nil
	}
	styled[0] = append(styledLine{{Text: firstPrefix, Style: styleDim}}, styled[0]...)
	for index := 1; index < len(styled); index++ {
		styled[index] = append(styledLine{{Text: "  ", Style: styleDim}}, styled[index]...)
	}
	return styled
}

func styledLinesFromMarkdown(lines []MarkdownLine) []styledLine {
	result := make([]styledLine, 0, len(lines))
	for _, line := range lines {
		spans := append(append([]MarkdownSpan(nil), line.InitialIndent...), line.Spans...)
		styled := make(styledLine, 0, len(spans))
		for _, span := range spans {
			if span.Text != "" {
				styled = append(styled, styledSpan{Text: span.Text, Style: span.Style, Markdown: span.Markdown, Syntax: span.Syntax, Destination: span.Destination})
			}
		}
		result = append(result, styled)
	}
	return result
}

func rawLinesFromMarkdownSource(source string) []string {
	if source == "" {
		return nil
	}
	lines := strings.Split(source, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type PlanUpdateCell struct {
	Explanation string
	Items       []protocol.PlanItemArg
}

func NewPlanUpdateCell(update protocol.PlanUpdateEvent) HistoryCell {
	return PlanUpdateCell{
		Explanation: strings.TrimSpace(update.Explanation),
		Items:       append([]protocol.PlanItemArg(nil), update.Plan...),
	}
}

func (cell PlanUpdateCell) DisplayLines(HistoryRenderContext) []styledLine {
	lines := []styledLine{{{Text: "• ", Style: styleDim}, {Text: "Updated Plan", Style: styleBold}}}
	body := make([]styledLine, 0, len(cell.Items)+1)
	if cell.Explanation != "" {
		body = append(body, styledLine{{Text: cell.Explanation, Style: styleDim}})
	}
	if len(cell.Items) == 0 {
		body = append(body, styledLine{{Text: "(no steps provided)", Style: styleDim}})
	} else {
		for _, item := range cell.Items {
			marker := "□ "
			style := styleDim
			switch item.Status {
			case protocol.StepCompleted:
				marker = "✔ "
			case protocol.StepInProgress:
				style = styleAccent
			}
			body = append(body, styledLine{{Text: marker + item.Step, Style: style}})
		}
	}
	for index, line := range body {
		prefix := "    "
		if index == 0 {
			prefix = "  └ "
		}
		lines = append(lines, append(styledLine{{Text: prefix, Style: styleDim}}, line...))
	}
	return lines
}

func (cell PlanUpdateCell) RawLines() []string {
	lines := []string{"Updated Plan"}
	if cell.Explanation != "" {
		lines = append(lines, cell.Explanation)
	}
	if len(cell.Items) == 0 {
		return append(lines, "(no steps provided)")
	}
	for _, item := range cell.Items {
		lines = append(lines, string(item.Status)+": "+item.Step)
	}
	return lines
}

func (PlanUpdateCell) IsStreamContinuation() bool { return false }
