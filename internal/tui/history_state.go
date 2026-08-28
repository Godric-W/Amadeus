package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model *appModel) shouldRenderRuntimeUserMessage(item protocol.TurnItem) bool {
	clientID := strings.TrimSpace(item.ClientUserMessageID)
	if clientID == "" {
		return true
	}
	if model.seenRuntimeUserMessages == nil {
		model.seenRuntimeUserMessages = make(map[string]struct{})
	}
	if _, exists := model.seenRuntimeUserMessages[clientID]; exists {
		return false
	}
	model.seenRuntimeUserMessages[clientID] = struct{}{}
	if _, optimistic := model.optimisticUserMessages[clientID]; optimistic {
		delete(model.optimisticUserMessages, clientID)
		return false
	}
	return true
}

func (model *appModel) restoreCompletedItems(items []protocol.TurnItem) {
	if model == nil {
		return
	}
	threadID := model.session.ThreadID
	for _, item := range items {
		switch item.Kind {
		case protocol.ItemUserMessage:
			if !model.shouldRenderRuntimeUserMessage(item) {
				continue
			}
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewUserMessageCell(item.Text))
		case protocol.ItemAssistantMessage:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewAgentMarkdownCell(newMarkdownSource(item.Text, model.session.Configuration.CWD)))
		case protocol.ItemReasoning:
		case protocol.ItemPlan:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewProposedPlanCell(newMarkdownSource(item.Text, model.session.Configuration.CWD)))
		case protocol.ItemContextCompaction:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewContextCompactedCell())
		default:
			model.applyEvent(protocol.Event{ID: "replay", Msg: protocol.ItemCompletedEvent{ThreadID: threadID, Item: item}})
		}
	}
	if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
		model.flushActiveHistoryCell()
	}
}

func (model *appModel) flushCompletedActivityBeforeBoundary() {
	if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
		model.flushActiveHistoryCell()
	}
}

func (model *appModel) finishTurn(elapsed time.Duration) {
	if model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
		model.insertHistoryCell(FinalMessageSeparator{Elapsed: elapsed})
	}
	model.transcript.HadWorkActivity = false
	model.transcript.NeedsFinalMessageSeparator = false
}

func collaborationModeName(mode protocol.ModeKind) string {
	if mode == protocol.ModeKindPlan {
		return "Plan"
	}
	return "Default"
}

func collaborationModeChangedMessage(mode protocol.ModeKind) string {
	return "Mode changed to " + collaborationModeName(mode) + "."
}

func (model *appModel) beginFinalMessage() {
	if !model.transcript.HadWorkActivity || !model.transcript.NeedsFinalMessageSeparator {
		return
	}
	model.flushActiveHistoryCell()
	model.insertHistoryCellNow(FinalMessageSeparator{})
	model.transcript.NeedsFinalMessageSeparator = false
}

func (model *appModel) insertHistoryCell(cell HistoryCell) {
	if model == nil || cell == nil {
		return
	}
	if model.markdownStreams.protectingTranscript() {
		model.markdownStreams.deferHistoryCell(cell)
		return
	}
	model.insertHistoryCellNow(cell)
}

func (model *appModel) insertHistoryCellNow(cell HistoryCell) {
	if model == nil || cell == nil {
		return
	}
	if len(cell.RawLines()) == 0 && len(cell.DisplayLines(model.historyRenderContext())) == 0 {
		return
	}
	model.historyCells = append(model.historyCells, cell)
}

func (model *appModel) insertStreamHistoryCell(cell HistoryCell) {
	if model == nil || cell == nil {
		return
	}
	if len(cell.RawLines()) == 0 && len(cell.DisplayLines(model.historyRenderContext())) == 0 {
		return
	}
	model.historyCells = append(model.historyCells, cell)
}

func (model *appModel) flushActiveHistoryCell() {
	if model == nil || model.transcript.ActiveCell == nil {
		return
	}
	model.insertHistoryCell(model.transcript.ActiveCell.Complete())
	model.transcript.ActiveCell = nil
	model.transcript.bumpActiveCellRevision()
}

func (model *appModel) resetHistory() {
	if model == nil {
		return
	}
	model.clearMarkdownStreams()
	model.markdownStreams.deferred = nil
	model.transcript.reset()
	model.TranscriptSurface.reset()
}

func (model appModel) latestAgentMarkdown() string {
	for index := len(model.historyCells) - 1; index >= 0; index-- {
		if cell, ok := model.historyCells[index].(*AgentMarkdownCell); ok {
			return cell.Source.Text
		}
	}
	return ""
}

func (model appModel) historyRenderContext() HistoryRenderContext {
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	return HistoryRenderContext{
		Width: maxInt(36, model.width-3), Palette: model.palette, Hyperlinks: model.hyperlinks && !model.palette.NoColor,
		Now: now, MotionStart: model.motionStartedAt, Motion: model.motion,
	}
}

func (model appModel) renderHistoryCell(cell HistoryCell) (rendered string) {
	if cell == nil {
		return ""
	}
	rendered = renderStyledLines(historyLinesForMode(cell, model.historyMode, model.historyRenderContext()), model.historyRenderContext())
	if model.app != nil && model.app.options.NoColor {
		return xansi.Strip(rendered)
	}
	return rendered
}

func (model *appModel) recallHistory(direction int) bool {
	if len(model.history) == 0 || strings.Contains(model.input.Value(), "\n") {
		return false
	}
	if model.historyPos < 0 {
		if direction > 0 {
			return false
		}
		model.historyPos = len(model.history) - 1
	} else {
		model.historyPos += direction
		if model.historyPos < 0 {
			model.historyPos = 0
		}
		if model.historyPos >= len(model.history) {
			model.historyPos = -1
			model.input.Reset()
			return true
		}
	}
	model.input.SetValue(model.history[model.historyPos])
	model.input.CursorEnd()
	model.updateInputLayout()
	return true
}

func (model *appModel) completeSlashCommand() bool {
	value := strings.TrimSpace(model.input.Value())
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		return false
	}
	matches := make([]string, 0)
	for _, command := range SlashCommands() {
		if strings.HasPrefix(command, value) {
			matches = append(matches, command)
		}
	}
	if len(matches) != 1 {
		return false
	}
	model.input.SetValue(matches[0])
	model.input.CursorEnd()
	return true
}
