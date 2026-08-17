package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model *fullscreenModel) applyEvent(event protocol.SessionEvent) {
	if model.runtimeTranscript == nil {
		model.runtimeTranscript = protocol.NewTranscriptState(event.ThreadID)
	}
	if err := model.runtimeTranscript.Apply(event); err != nil {
		// A live provider may emit a delta before the UI observes its start
		// marker (for example when an adapter is attached mid-stream). Keep the
		// strict reducer diagnostic, but recover the projection locally so text
		// is not lost. Canonical replay never takes this path.
		if recovered := model.recoverDeltaStart(event); recovered {
			_ = model.runtimeTranscript.Apply(event)
		} else {
			model.insertHistoryCell(NewDiagnosticHistoryCell("event projection: " + err.Error()))
			return
		}
	}
	message := event.Message
	switch item := message.(type) {
	case protocol.TurnStarted:
		model.status = "working"
	case protocol.AssistantMessageDelta:
		if model.draft == "" && model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
			model.flushActiveHistoryCell()
			model.insertHistoryCell(FinalMessageSeparator{Elapsed: model.runElapsed()})
			model.transcript.NeedsFinalMessageSeparator = false
		}
		model.draft += item.Delta
	case protocol.ReasoningDelta:
		if strings.TrimSpace(item.Delta) != "" {
			model.status = "thinking"
		}
	case protocol.ThreadTokenUsageUpdated:
		model.inputUsage = item.Usage.InputTokens
		model.outputUsage = item.Usage.OutputTokens
	case protocol.PlanUpdated:
		model.finishDraft()
		model.insertHistoryCell(NewPlanUpdateCell(item))
		model.status = "planning"
	case protocol.ItemStarted:
		if item.Item.ToolName == "update_plan" {
			return
		}
		model.finishDraft()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		if model.transcript.ActiveCell == nil {
			model.transcript.ActiveCell = newToolHistoryCell()
		}
		if model.transcript.ActiveCell.Apply(item) {
			model.transcript.bumpActiveCellRevision()
		}
		model.transcript.HadWorkActivity = true
		model.transcript.NeedsFinalMessageSeparator = true
		model.status = "executing"
	case protocol.ItemCompleted:
		if item.Item.ToolName == "update_plan" {
			return
		}
		model.finishDraft()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		if model.transcript.ActiveCell == nil {
			cell := newToolHistoryCell()
			cell.Apply(protocol.ItemStarted{Item: protocol.TurnItem{ID: item.Item.ID, Kind: item.Item.Kind, Status: protocol.ItemInProgress, CreatedAt: item.Item.CreatedAt, ToolName: item.Item.ToolName, CallID: item.Item.CallID, Payload: item.Item.Payload}})
			model.transcript.ActiveCell = cell
		}
		if model.transcript.ActiveCell.Apply(item) {
			model.transcript.bumpActiveCellRevision()
		}
		model.transcript.HadWorkActivity = true
		model.transcript.NeedsFinalMessageSeparator = true
		if model.details == nil {
			model.details = newTranscriptDetailStore(0, 0)
		}
		if cell, ok := model.transcript.ActiveCell.(*ToolHistoryCell); ok {
			activity := cell.byCallID[item.Item.CallID]
			if activity != nil {
				detailContent := toolActivityDetailContent(activity, item.Item.Text)
				if detail, ok := model.details.Add(item.Item.CallID, activity.Title, detailContent); ok {
					activity.ResultDetailID = detail.ID
					activity.DetailAvailable = isExploreTool(activity.ToolName) || activityFileChangePreview(activity) != nil || detail.Truncated || strings.Count(detailContent, "\n") >= 5 || len([]rune(detailContent)) > 600
					activity.Result = detail.Content
				}
			}
		}
	case protocol.Warning:
		model.finishDraft()
		model.insertHistoryCell(NewDiagnosticHistoryCell(item.Message))
	case protocol.StreamError:
		model.finishDraft()
		if strings.TrimSpace(item.Error) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Error))
		}
	case protocol.TurnCompleted:
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.status = "completed"
	case protocol.TurnAborted:
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.status = "aborted"
	}
}

func (model *fullscreenModel) recoverDeltaStart(event protocol.SessionEvent) bool {
	var itemID string
	var kind protocol.ItemKind
	switch message := event.Message.(type) {
	case protocol.AssistantMessageDelta:
		itemID, kind = message.ItemID, protocol.ItemAssistantMessage
	case protocol.ReasoningDelta:
		itemID, kind = message.ItemID, protocol.ItemReasoning
	case protocol.CommandOutputDelta:
		itemID, kind = message.ItemID, protocol.ItemCommandExecution
	default:
		return false
	}
	if strings.TrimSpace(itemID) == "" {
		return false
	}
	now := time.Now().UTC()
	return model.runtimeTranscript.Apply(protocol.SessionEvent{
		ThreadID: event.ThreadID, TurnID: event.TurnID,
		Message: protocol.ItemStarted{Item: protocol.TurnItem{ID: itemID, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: now}},
	}) == nil
}

func (model *fullscreenModel) restoreCompletedItems(items []protocol.TurnItem) {
	if model == nil {
		return
	}
	threadID := rollout.ThreadID(model.startup.Session)
	for _, item := range items {
		if item.Kind == protocol.ItemAssistantMessage {
			model.insertHistoryCell(NewAgentMessageCell(item.Text))
			continue
		}
		if item.Kind == protocol.ItemReasoning {
			continue
		}
		event := protocol.SessionEvent{ThreadID: threadID, Message: protocol.ItemCompleted{Item: item}}
		model.applyEvent(event)
	}
	if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
		model.flushActiveHistoryCell()
	}
}

func (model *fullscreenModel) finishDraft() {
	if strings.TrimSpace(model.draft) != "" {
		model.transcript.LastAgentMarkdown = model.draft
		model.insertHistoryCell(NewAgentMessageCell(model.draft))
	}
	model.draft = ""
}

func (model *fullscreenModel) insertHistoryCell(cell HistoryCell) {
	if model == nil || cell == nil {
		return
	}
	if len(cell.RawLines()) == 0 && len(cell.DisplayLines(model.historyRenderContext())) == 0 {
		return
	}
	model.historyCells = append(model.historyCells, cell)
	model.pendingHistoryCells = append(model.pendingHistoryCells, cell)
}

func (model *fullscreenModel) flushActiveHistoryCell() {
	if model == nil || model.transcript.ActiveCell == nil {
		return
	}
	model.insertHistoryCell(model.transcript.ActiveCell.Complete())
	model.transcript.ActiveCell = nil
	model.transcript.bumpActiveCellRevision()
}

func (model *fullscreenModel) resetHistory() {
	if model == nil {
		return
	}
	model.transcript.reset()
	model.historyCells = nil
	model.pendingHistoryCells = nil
	model.hasEmittedHistoryLines = false
}

func (model fullscreenModel) historyRenderContext() HistoryRenderContext {
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	return HistoryRenderContext{
		Width: maxInt(36, model.width-3), Palette: model.palette, Markdown: model.renderer,
		Now: now, MotionStart: model.motionStartedAt, Motion: model.motion,
	}
}

func (model fullscreenModel) renderHistoryCell(cell HistoryCell) (rendered string) {
	if cell == nil {
		return ""
	}
	rendered = renderStyledLines(historyLinesForMode(cell, model.historyMode, model.historyRenderContext()), model.historyRenderContext())
	if model.app != nil && model.app.options.NoColor {
		return xansi.Strip(rendered)
	}
	return rendered
}

func (model *fullscreenModel) recallHistory(direction int) bool {
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

func (model *fullscreenModel) completeSlashCommand() bool {
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
