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

type AgentMessageCell struct{ Markdown string }

func NewAgentMessageCell(markdown string) HistoryCell {
	return AgentMessageCell{Markdown: strings.TrimSpace(markdown)}
}

func (cell AgentMessageCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	content := sanitizeContent(cell.Markdown)
	if content == "" {
		return nil
	}
	rendered := prefixRenderedBlock(content, "• ")
	if ctx.Markdown != nil {
		if markdown, err := ctx.Markdown.Render(content); err == nil {
			rendered = prefixRenderedBlock(markdown, "• ")
		}
	}
	return styledLinesFromText(rendered, styleRendered)
}

func (cell AgentMessageCell) RawLines() []string {
	if strings.TrimSpace(cell.Markdown) == "" {
		return nil
	}
	return strings.Split(cell.Markdown, "\n")
}

func (AgentMessageCell) IsStreamContinuation() bool { return false }

type ProposedPlanCell struct{ Markdown string }

func NewProposedPlanCell(markdown string) HistoryCell {
	return ProposedPlanCell{Markdown: strings.TrimSpace(markdown)}
}

func (cell ProposedPlanCell) DisplayLines(ctx HistoryRenderContext) []styledLine {
	content := sanitizeContent(cell.Markdown)
	if content == "" {
		return nil
	}
	body := content
	if ctx.Markdown != nil {
		if rendered, err := ctx.Markdown.Render(content); err == nil {
			body = rendered
		}
	}
	return append([]styledLine{{{Text: "• ", Style: styleDim}, {Text: "Proposed Plan", Style: styleBold}}}, styledLinesFromText(prefixRenderedBlock(body, "  "), styleRendered)...)
}

func (cell ProposedPlanCell) RawLines() []string {
	if cell.Markdown == "" {
		return nil
	}
	return append([]string{"Proposed Plan"}, strings.Split(cell.Markdown, "\n")...)
}
func (ProposedPlanCell) IsStreamContinuation() bool { return false }

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
