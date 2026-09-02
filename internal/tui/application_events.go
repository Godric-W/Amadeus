package tui

import (
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func (model *appModel) applyEvent(event protocol.Event) tea.Cmd {
	if streamError, retrying := event.Msg.(protocol.StreamErrorEvent); !retrying || !streamError.WillRetry {
		model.restoreRetryStatus()
	}
	if model.protocolEvents == nil {
		model.protocolEvents = newProtocolEventState(protocol.ThreadIDOf(event.Msg))
	}
	if err := model.protocolEvents.Apply(event); err != nil {
		model.insertHistoryCell(NewDiagnosticHistoryCell("event projection: " + err.Error()))
		return nil
	}
	if model.shouldDeferProtocolProjection(event.Msg) {
		model.markdownStreams.deferProtocol(event.Msg)
		return nil
	}
	return model.projectProtocolEvent(event.Msg)
}

func (model *appModel) projectProtocolEvent(message protocol.EventMsg) tea.Cmd {
	switch item := message.(type) {
	case protocol.SessionConfiguredEvent:
		return model.applySessionConfigured(item)
	case protocol.TurnStartedEvent:
		model.clearRetryStatus()
		model.nextTurnQueue.ConfirmStarted(model.session.ThreadID, model.session.Generation)
		model.running = true
		model.runStartedAt = item.StartedAt
		if model.runStartedAt.IsZero() {
			model.runStartedAt = time.Now()
		}
		model.motionStartedAt = model.runStartedAt
		if model.session.Goal != nil && model.session.Goal.Status == protocol.ThreadGoalActive {
			model.goalActiveTurnStartedAt = model.runStartedAt
		}
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		if model.status != "compacting context" {
			if model.session.mode() == protocol.ModeKindPlan {
				model.status = "planning"
			} else {
				model.status = "working"
			}
		}
		return model.workingTick()
	case protocol.ThreadSettingsAppliedEvent:
		previousMode := model.session.mode()
		command := model.applySessionConfiguration(item.Configuration)
		model.pendingMode = ""
		model.status = "idle"
		if previousMode != model.session.mode() {
			model.insertHistoryCell(NewInfoHistoryCell(collaborationModeChangedMessage(model.session.mode())))
		}
		return command
	case protocol.AgentMessageContentDeltaEvent:
		if model.markdownStreams.assistant == nil {
			model.insertHistoryCell(NewDiagnosticHistoryCell("assistant delta without active stream"))
			return nil
		}
		if model.markdownStreams.assistant.Source.Source() == "" && item.Delta != "" {
			model.beginFinalMessage()
			if err := model.TranscriptSurface.beginStream(item.ItemID, protocol.ItemAssistantMessage); err != nil {
				model.insertHistoryCell(NewDiagnosticHistoryCell("assistant stream attachment: " + err.Error()))
				return nil
			}
			model.markdownStreams.transcriptProtected = true
		}
		if item.Reset {
			model.TranscriptSurface.rewindStream(item.ItemID)
		}
		if _, err := model.markdownStreams.assistant.Push(item.ItemID, item.Delta, item.Reset); err != nil {
			model.insertHistoryCell(NewDiagnosticHistoryCell("assistant stream: " + err.Error()))
		} else {
			model.commitMarkdownStream(model.markdownStreams.assistant)
		}
	case protocol.ReasoningContentDeltaEvent:
		if !item.Reset && strings.TrimSpace(item.Delta) != "" {
			model.status = "thinking"
		}
	case protocol.TokenCountEvent:
		model.session.applyTokenCount(item)
		model.refreshStatusLine()
	case protocol.ThreadGoalUpdatedEvent:
		model.setGoalSnapshot(&item.Goal, model.uiNow())
	case protocol.ThreadGoalClearedEvent:
		model.setGoalSnapshot(nil, model.uiNow())
	case protocol.PlanUpdateEvent:
		model.insertHistoryCell(NewPlanUpdateCell(item))
		model.status = "planning"
	case protocol.PlanDeltaEvent:
		if model.markdownStreams.plan == nil {
			model.insertHistoryCell(NewDiagnosticHistoryCell("plan delta without active stream"))
			return nil
		}
		if model.markdownStreams.plan.Source.Source() == "" && item.Delta != "" {
			if err := model.TranscriptSurface.beginStream(item.ItemID, protocol.ItemPlan); err != nil {
				model.insertHistoryCell(NewDiagnosticHistoryCell("plan stream attachment: " + err.Error()))
				return nil
			}
			model.markdownStreams.transcriptProtected = true
		}
		if _, err := model.markdownStreams.plan.Push(item.ItemID, item.Delta, false); err != nil {
			model.insertHistoryCell(NewDiagnosticHistoryCell("plan stream: " + err.Error()))
		} else {
			model.commitMarkdownStream(model.markdownStreams.plan.StreamController)
		}
		model.status = "planning"
	case protocol.ItemStartedEvent:
		if item.Item.Kind == protocol.ItemCollabAgentToolCall {
			model.clearMarkdownStreams()
			if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
				model.flushActiveHistoryCell()
			}
			if _, ok := model.transcript.ActiveCell.(*CollabAgentHistoryCell); !ok {
				if model.transcript.ActiveCell != nil {
					model.flushActiveHistoryCell()
				}
				model.transcript.ActiveCell = newCollabAgentHistoryCell()
			}
			if model.transcript.ActiveCell.Apply(item) {
				model.transcript.bumpActiveCellRevision()
			}
			model.transcript.HadWorkActivity = true
			model.transcript.NeedsFinalMessageSeparator = true
			model.status = "working"
			return nil
		}
		switch item.Item.Kind {
		case protocol.ItemPlan:
			if err := model.startPlanStream(item.Item); err != nil {
				model.insertHistoryCell(NewDiagnosticHistoryCell("start plan stream: " + err.Error()))
			}
			return nil
		case protocol.ItemContextCompaction:
			model.status = "compacting context"
			return nil
		case protocol.ItemAssistantMessage:
			if err := model.startAssistantStream(item.Item); err != nil {
				model.insertHistoryCell(NewDiagnosticHistoryCell("start assistant stream: " + err.Error()))
			}
			return nil
		case protocol.ItemReasoning, protocol.ItemUserMessage:
			return nil
		}
		model.clearMarkdownStreams()
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
		if item.Item.Kind == protocol.ItemCollabAgentToolCall {
			model.clearMarkdownStreams()
			if _, ok := model.transcript.ActiveCell.(*CollabAgentHistoryCell); !ok {
				if model.transcript.ActiveCell != nil {
					model.flushActiveHistoryCell()
				}
				cell := newCollabAgentHistoryCell()
				cell.Apply(protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: item.Item.ID, Kind: item.Item.Kind, Status: protocol.ItemInProgress, CreatedAt: item.Item.CreatedAt, ToolName: item.Item.ToolName, CallID: item.Item.CallID, Payload: item.Item.Payload}})
				model.transcript.ActiveCell = cell
			}
			if model.transcript.ActiveCell.Apply(item) {
				model.transcript.bumpActiveCellRevision()
			}
			model.transcript.HadWorkActivity = true
			model.transcript.NeedsFinalMessageSeparator = true
			return nil
		}
		switch item.Item.Kind {
		case protocol.ItemUserMessage:
			if !model.shouldRenderRuntimeUserMessage(item.Item) {
				return nil
			}
			model.flushCompletedActivityBeforeBoundary()
			model.insertHistoryCell(NewUserMessageCell(item.Item.Text))
			return nil
		case protocol.ItemAssistantMessage:
			model.flushCompletedActivityBeforeBoundary()
			source := newMarkdownSource(item.Item.Text, model.session.Configuration.CWD)
			if model.markdownStreams.assistant != nil && model.markdownStreams.assistant.ItemID == item.Item.ID {
				source = model.markdownStreams.assistant.Finalize(item.Item.Text)
			}
			if model.markdownStreams.assistant == nil || model.markdownStreams.assistant.ItemID != item.Item.ID {
				model.beginFinalMessage()
			}
			model.TranscriptSurface.consolidateStream(item.Item.ID, NewAgentMarkdownCell(source))
			model.markdownStreams.assistant = nil
			model.markdownStreams.transcriptProtected = false
			if model.transcript.HadWorkActivity {
				model.transcript.NeedsFinalMessageSeparator = true
			}
			return model.flushDeferredTranscriptProjections()
		case protocol.ItemReasoning:
			return nil
		case protocol.ItemPlan:
			model.flushCompletedActivityBeforeBoundary()
			source := newMarkdownSource(item.Item.Text, model.session.Configuration.CWD)
			if model.markdownStreams.plan != nil && model.markdownStreams.plan.ItemID == item.Item.ID {
				source = model.markdownStreams.plan.Finalize(item.Item.Text)
			}
			model.TranscriptSurface.consolidateStream(item.Item.ID, NewProposedPlanCell(source))
			model.markdownStreams.plan = nil
			model.markdownStreams.transcriptProtected = false
			model.completedProposedPlan = true
			return model.flushDeferredTranscriptProjections()
		case protocol.ItemContextCompaction:
			model.flushCompletedActivityBeforeBoundary()
			if item.Item.Status == protocol.ItemStatusCompleted {
				model.insertHistoryCell(NewContextCompactedCell())
			} else {
				model.insertHistoryCell(NewErrorHistoryCell(item.Item.Text))
			}
			return nil
		}
		model.clearMarkdownStreams()
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
		model.insertHistoryCell(NewWarningHistoryCell(item.Message))
	case protocol.StreamErrorEvent:
		if item.WillRetry {
			model.showRetryStatus(item)
			return model.workingTick()
		}
		model.clearMarkdownStreams()
		deferred := model.flushDeferredTranscriptProjections()
		if strings.TrimSpace(item.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Message))
		}
		return deferred
	case protocol.TurnCompleteEvent:
		model.goalActiveTurnStartedAt = time.Time{}
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.clearMarkdownStreams()
		deferred := model.flushDeferredTranscriptProjections()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "completed"
		if item.Outcome == protocol.TurnOutcomeBlocked || model.nextTurnQueue.Halted() {
			model.restoreQueuedInputsToComposer()
			model.completedProposedPlan = false
			return deferred
		}
		if model.nextTurnQueue.HasPending() {
			model.completedProposedPlan = false
			return tea.Sequence(deferred, model.maybeSubmitNextQueuedInput())
		}
		if !model.nextTurnQueue.HasQueuedFollowUp() && model.session.mode() == protocol.ModeKindPlan && model.completedProposedPlan && model.approval == nil && model.userInputDialog == nil {
			model.selection = &selectionOverlay{Title: "Implement this plan?", Items: []selectionItem{{Name: "Implement this plan", Description: "Switch to Default mode and start implementation"}, {Name: "Stay in Plan mode", Description: "Keep planning without starting implementation"}}}
			model.selectionKind = "implement-plan"
			model.completedProposedPlan = false
		}
		return deferred
	case protocol.TurnAbortedEvent:
		model.goalActiveTurnStartedAt = time.Time{}
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.clearMarkdownStreams()
		deferred := model.flushDeferredTranscriptProjections()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "aborted"
		model.restoreQueuedInputsToComposer()
		return deferred
	case protocol.ErrorEvent:
		model.clearRetryStatus()
		model.clearMarkdownStreams()
		deferred := model.flushDeferredTranscriptProjections()
		if !model.running {
			model.status = "idle"
		}
		if strings.TrimSpace(item.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Message))
		}
		return deferred
	case protocol.ShutdownCompleteEvent:
		model.status = "shutting down"
	}
	return nil
}
