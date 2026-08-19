package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model *fullscreenModel) applyEvent(event protocol.SessionEvent) tea.Cmd {
	if streamError, retrying := event.Message.(protocol.StreamError); !retrying || !streamError.WillRetry {
		model.restoreRetryStatus()
	}
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
			return nil
		}
	}
	message := event.Message
	switch item := message.(type) {
	case protocol.TurnStarted:
		model.clearRetryStatus()
		model.running = true
		model.runStartedAt = item.StartedAt
		if model.runStartedAt.IsZero() {
			model.runStartedAt = time.Now()
		}
		model.motionStartedAt = model.runStartedAt
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		if item.Kind == protocol.TaskKindCompact {
			model.status = "compacting context"
		} else if model.collaboration == CollaborationPlan {
			model.status = "planning"
		} else {
			model.status = "working"
		}
		return model.workingTick()
	case protocol.ThreadSettingsUpdated:
		if item.Mode == string(turn.ModeKindPlan) {
			model.collaboration = CollaborationPlan
			model.status = "plan mode"
		} else {
			model.collaboration = CollaborationExecute
			model.status = "idle"
		}
		task := strings.TrimSpace(model.pendingModeTask)
		model.pendingModeTask = ""
		if task == "" {
			model.insertHistoryCell(NewNoticeHistoryCell("Switched to " + collaborationModeName(model.collaboration) + " mode"))
			return nil
		}
		model.insertHistoryCell(NewUserMessageCell(task))
		model.running = true
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		model.status = taskPhase(TaskSubmission{Content: task, Mode: model.collaboration})
		return tea.Batch(model.submitTask(TaskSubmission{Content: task, Mode: model.collaboration}), model.workingTick())
	case protocol.AssistantMessageDelta:
		if item.Reset {
			model.draft = item.Delta
			break
		}
		if model.draft == "" && model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
			model.flushActiveHistoryCell()
			model.insertHistoryCell(FinalMessageSeparator{Elapsed: model.runElapsed()})
			model.transcript.NeedsFinalMessageSeparator = false
		}
		model.draft += item.Delta
	case protocol.ReasoningDelta:
		if !item.Reset && strings.TrimSpace(item.Delta) != "" {
			model.status = "thinking"
		}
	case protocol.ThreadTokenUsageUpdated:
		model.inputUsage = item.Usage.InputTokens
		model.outputUsage = item.Usage.OutputTokens
		if item.EstimatedInputTokens > 0 {
			model.contextUsage = item.EstimatedInputTokens
		}
		if item.ContextWindow > 0 {
			model.contextLimit = item.ContextWindow
		}
		if item.Usage.InputTokens > 0 {
			model.contextUsage = item.Usage.InputTokens
		}
	case protocol.PlanUpdated:
		model.finishDraft()
		model.insertHistoryCell(NewPlanUpdateCell(item))
		model.status = "planning"
	case protocol.ItemStarted:
		if item.Item.ToolName == "update_plan" {
			return nil
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
		switch item.Item.Kind {
		case protocol.ItemUserMessage:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewUserMessageCell(item.Item.Text))
			return nil
		case protocol.ItemAssistantMessage:
			model.flushCompletedActivityBeforeBoundary()
			if strings.TrimSpace(model.draft) != "" {
				model.finishDraft()
			} else if strings.TrimSpace(item.Item.Text) != "" {
				model.transcript.LastAgentMarkdown = item.Item.Text
				model.insertHistoryCell(NewAgentMessageCell(item.Item.Text))
			}
			return nil
		case protocol.ItemReasoning:
			return nil
		case protocol.ItemPlan:
			model.flushCompletedActivityBeforeBoundary()
			if update, ok := item.Item.Payload.(protocol.PlanUpdated); ok {
				model.insertHistoryCell(NewPlanUpdateCell(update))
			}
			return nil
		case protocol.ItemContextCompaction:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewContextCompactedCell())
			return nil
		}
		if item.Item.ToolName == "update_plan" {
			return nil
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
		model.insertHistoryCell(NewWarningHistoryCell(item.Message))
	case protocol.StreamError:
		if item.WillRetry {
			model.showRetryStatus(item)
			return model.workingTick()
		}
		model.finishDraft()
		if strings.TrimSpace(item.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Message))
		}
	case protocol.TurnCompleted:
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "completed"
	case protocol.TurnAborted:
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "aborted"
	case protocol.TurnRejected:
		model.clearRetryStatus()
		model.finishDraft()
		model.running = false
		model.status = "idle"
		if strings.TrimSpace(item.Error) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Error))
		}
	case protocol.ContextCompacted:
		model.insertHistoryCell(NewContextCompactedCell())
	case protocol.ShutdownComplete:
		model.status = "shutting down"
	}
	return nil
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
		switch item.Kind {
		case protocol.ItemUserMessage:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewUserMessageCell(item.Text))
		case protocol.ItemAssistantMessage:
			model.flushCompletedActivityBeforeBoundary()
			model.transcript.LastAgentMarkdown = item.Text
			model.insertHistoryCell(NewAgentMessageCell(item.Text))
		case protocol.ItemReasoning:
		case protocol.ItemPlan:
			model.flushCompletedActivityBeforeBoundary()
			if update, ok := item.Payload.(protocol.PlanUpdated); ok {
				model.insertHistoryCell(NewPlanUpdateCell(update))
			}
		case protocol.ItemContextCompaction:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewContextCompactedCell())
		default:
			event := protocol.SessionEvent{ThreadID: threadID, Message: protocol.ItemCompleted{Item: item}}
			model.applyEvent(event)
		}
	}
	if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
		model.flushActiveHistoryCell()
	}
}

func (model *fullscreenModel) flushCompletedActivityBeforeBoundary() {
	if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
		model.flushActiveHistoryCell()
	}
}

func (model *fullscreenModel) finishTurn(elapsed time.Duration) {
	if model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
		model.insertHistoryCell(FinalMessageSeparator{Elapsed: elapsed})
	}
	model.transcript.HadWorkActivity = false
	model.transcript.NeedsFinalMessageSeparator = false
}

func collaborationModeName(mode CollaborationMode) string {
	if mode == CollaborationPlan {
		return "Plan"
	}
	return "Execute"
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
