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

func (model *appModel) recoverDeltaStart(event protocol.Event) bool {
	var itemID protocol.ItemID
	var kind protocol.ItemKind
	switch message := event.Msg.(type) {
	case protocol.AgentMessageContentDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemAssistantMessage
	case protocol.ReasoningContentDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemReasoning
	case protocol.CommandOutputDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemCommandExecution
	case protocol.PlanDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemPlan
	default:
		return false
	}
	if strings.TrimSpace(string(itemID)) == "" {
		return false
	}
	now := time.Now().UTC()
	return model.protocolEvents.Apply(protocol.Event{
		ID: event.ID,
		Msg: protocol.ItemStartedEvent{
			ThreadID: protocol.ThreadIDOf(event.Msg), TurnID: protocol.TurnIDOf(event.Msg),
			Item: protocol.TurnItem{ID: itemID, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: now},
		},
	}) == nil
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
			model.transcript.LastAgentMarkdown = item.Text
			model.insertHistoryCell(NewAgentMessageCell(item.Text))
		case protocol.ItemReasoning:
		case protocol.ItemPlan:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewProposedPlanCell(item.Text))
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
	if model.draft != "" || !model.transcript.HadWorkActivity || !model.transcript.NeedsFinalMessageSeparator {
		return
	}
	model.flushActiveHistoryCell()
	model.insertHistoryCell(FinalMessageSeparator{})
	model.transcript.NeedsFinalMessageSeparator = false
}

func (model *appModel) finishDraft() {
	if strings.TrimSpace(model.draft) != "" {
		model.transcript.LastAgentMarkdown = model.draft
		model.insertHistoryCell(NewAgentMessageCell(model.draft))
		if model.transcript.HadWorkActivity {
			model.transcript.NeedsFinalMessageSeparator = true
		}
	}
	model.draft = ""
}

func (model *appModel) insertHistoryCell(cell HistoryCell) {
	if model == nil || cell == nil {
		return
	}
	if len(cell.RawLines()) == 0 && len(cell.DisplayLines(model.historyRenderContext())) == 0 {
		return
	}
	model.historyCells = append(model.historyCells, cell)
	model.pendingHistoryCells = append(model.pendingHistoryCells, cell)
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
	model.transcript.reset()
	model.historyCells = nil
	model.pendingHistoryCells = nil
	model.hasEmittedHistoryLines = false
}

func (model appModel) historyRenderContext() HistoryRenderContext {
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	return HistoryRenderContext{
		Width: maxInt(36, model.width-3), Palette: model.palette, Markdown: model.renderer,
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
