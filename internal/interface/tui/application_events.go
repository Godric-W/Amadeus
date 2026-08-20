package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	runtimeprojection "github.com/Godric-W/Amadeus/internal/app/transcript"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func (model *fullscreenModel) applyEvent(event protocol.Event) tea.Cmd {
	if streamError, retrying := event.Msg.(protocol.StreamErrorEvent); !retrying || !streamError.WillRetry {
		model.restoreRetryStatus()
	}
	if model.runtimeTranscript == nil {
		model.runtimeTranscript = runtimeprojection.New(protocol.ThreadIDOf(event.Msg))
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
	message := event.Msg
	switch item := message.(type) {
	case protocol.TurnStartedEvent:
		model.clearRetryStatus()
		model.running = true
		model.runStartedAt = item.StartedAt
		if model.runStartedAt.IsZero() {
			model.runStartedAt = time.Now()
		}
		model.motionStartedAt = model.runStartedAt
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		if model.status != "compacting context" {
			if model.collaboration == CollaborationPlan {
				model.status = "planning"
			} else {
				model.status = "working"
			}
		}
		return model.workingTick()
	case protocol.ThreadSettingsAppliedEvent:
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
	case protocol.AgentMessageContentDeltaEvent:
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
	case protocol.ReasoningContentDeltaEvent:
		if !item.Reset && strings.TrimSpace(item.Delta) != "" {
			model.status = "thinking"
		}
	case protocol.TokenCountEvent:
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
	case protocol.PlanUpdateEvent:
		model.finishDraft()
		model.insertHistoryCell(NewPlanUpdateCell(item))
		model.status = "planning"
	case protocol.ItemStartedEvent:
		switch item.Item.Kind {
		case protocol.ItemAssistantMessage, protocol.ItemReasoning, protocol.ItemUserMessage, protocol.ItemPlan, protocol.ItemContextCompaction:
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
			model.transcript.ActiveCell = newToolHistoryCell()
		}
		if model.transcript.ActiveCell.Apply(item) {
			model.transcript.bumpActiveCellRevision()
		}
		model.transcript.HadWorkActivity = true
		model.transcript.NeedsFinalMessageSeparator = true
		model.status = "working"
	case protocol.ItemCompletedEvent:
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
			if update, ok := item.Item.Payload.(protocol.PlanUpdateEvent); ok {
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
			cell.Apply(protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: item.Item.ID, Kind: item.Item.Kind, Status: protocol.ItemInProgress, CreatedAt: item.Item.CreatedAt, ToolName: item.Item.ToolName, CallID: item.Item.CallID, Payload: item.Item.Payload}})
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
	case protocol.WarningEvent:
		model.finishDraft()
		model.insertHistoryCell(NewWarningHistoryCell(item.Message))
	case protocol.StreamErrorEvent:
		if item.WillRetry {
			model.showRetryStatus(item)
			return model.workingTick()
		}
		model.finishDraft()
		if strings.TrimSpace(item.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Message))
		}
	case protocol.TurnCompleteEvent:
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "completed"
	case protocol.TurnAbortedEvent:
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "aborted"
	case protocol.ErrorEvent:
		model.clearRetryStatus()
		model.finishDraft()
		model.running = false
		model.status = "idle"
		if strings.TrimSpace(item.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Message))
		}
	case protocol.ContextCompactedEvent:
		model.insertHistoryCell(NewContextCompactedCell())
	case protocol.ShutdownCompleteEvent:
		model.status = "shutting down"
	}
	return nil
}

func (model *fullscreenModel) recoverDeltaStart(event protocol.Event) bool {
	var itemID protocol.ItemID
	var kind protocol.ItemKind
	switch message := event.Msg.(type) {
	case protocol.AgentMessageContentDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemAssistantMessage
	case protocol.ReasoningContentDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemReasoning
	case protocol.CommandOutputDeltaEvent:
		itemID, kind = message.ItemID, protocol.ItemCommandExecution
	default:
		return false
	}
	if strings.TrimSpace(string(itemID)) == "" {
		return false
	}
	now := time.Now().UTC()
	return model.runtimeTranscript.Apply(protocol.Event{
		ID:  event.ID,
		Msg: protocol.ItemStartedEvent{ThreadID: protocol.ThreadIDOf(event.Msg), TurnID: protocol.TurnIDOf(event.Msg), Item: protocol.TurnItem{ID: itemID, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: now}},
	}) == nil
}

func (model *fullscreenModel) restoreCompletedItems(items []protocol.TurnItem) {
	if model == nil {
		return
	}
	threadID := protocol.ThreadID(model.startup.Session)
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
			if update, ok := item.Payload.(protocol.PlanUpdateEvent); ok {
				model.insertHistoryCell(NewPlanUpdateCell(update))
			}
		case protocol.ItemContextCompaction:
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewContextCompactedCell())
		default:
			event := protocol.Event{ID: "replay", Msg: protocol.ItemCompletedEvent{ThreadID: threadID, Item: item}}
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
