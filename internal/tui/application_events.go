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
		// A live provider may emit a delta before the UI observes its start
		// marker (for example when an adapter is attached mid-stream). Keep the
		// strict reducer diagnostic, but recover the projection locally so text
		// is not lost. Canonical replay never takes this path.
		if recovered := model.recoverDeltaStart(event); recovered {
			_ = model.protocolEvents.Apply(event)
		} else {
			model.insertHistoryCell(NewDiagnosticHistoryCell("event projection: " + err.Error()))
			return nil
		}
	}
	message := event.Msg
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
		model.beginFinalMessage()
		if item.Reset {
			model.draft = item.Delta
			break
		}
		model.draft += item.Delta
	case protocol.ReasoningContentDeltaEvent:
		if !item.Reset && strings.TrimSpace(item.Delta) != "" {
			model.status = "thinking"
		}
	case protocol.TokenCountEvent:
		model.session.applyTokenCount(item)
		model.refreshStatusLine()
	case protocol.PlanUpdateEvent:
		model.finishDraft()
		model.insertHistoryCell(NewPlanUpdateCell(item))
		model.status = "planning"
	case protocol.PlanDeltaEvent:
		model.proposedPlanDraft += item.Delta
		model.status = "planning"
	case protocol.ItemStartedEvent:
		if item.Item.Kind == protocol.ItemCollabAgentToolCall {
			model.finishDraft()
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
			model.proposedPlanDraft = ""
			return nil
		case protocol.ItemContextCompaction:
			model.status = "compacting context"
			return nil
		case protocol.ItemAssistantMessage, protocol.ItemReasoning, protocol.ItemUserMessage:
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
		if item.Item.Kind == protocol.ItemCollabAgentToolCall {
			model.finishDraft()
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
			if strings.TrimSpace(model.draft) != "" {
				model.finishDraft()
			} else if strings.TrimSpace(item.Item.Text) != "" {
				model.beginFinalMessage()
				model.transcript.LastAgentMarkdown = item.Item.Text
				model.insertHistoryCell(NewAgentMessageCell(item.Item.Text))
				if model.transcript.HadWorkActivity {
					model.transcript.NeedsFinalMessageSeparator = true
				}
			}
			return nil
		case protocol.ItemReasoning:
			return nil
		case protocol.ItemPlan:
			model.flushCompletedActivityBeforeBoundary()
			model.proposedPlanDraft = ""
			model.completedProposedPlan = true
			model.insertHistoryCell(NewProposedPlanCell(item.Item.Text))
			return nil
		case protocol.ItemContextCompaction:
			model.flushCompletedActivityBeforeBoundary()
			if item.Item.Status == protocol.ItemStatusCompleted {
				model.insertHistoryCell(NewContextCompactedCell())
			} else {
				model.insertHistoryCell(NewErrorHistoryCell(item.Item.Text))
			}
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
		if item.Outcome == protocol.TurnOutcomeBlocked || model.nextTurnQueue.Halted() {
			model.restoreQueuedInputsToComposer()
			model.completedProposedPlan = false
			return nil
		}
		if model.nextTurnQueue.HasPending() {
			model.completedProposedPlan = false
			return model.maybeSubmitNextQueuedInput()
		}
		if !model.nextTurnQueue.HasQueuedFollowUp() && model.session.mode() == protocol.ModeKindPlan && model.completedProposedPlan && model.approval == nil && model.userInputDialog == nil {
			model.selection = &selectionOverlay{Title: "Implement this plan?", Items: []selectionItem{{Name: "Implement this plan", Description: "Switch to Default mode and start implementation"}, {Name: "Stay in Plan mode", Description: "Keep planning without starting implementation"}}}
			model.selectionKind = "implement-plan"
			model.completedProposedPlan = false
		}
	case protocol.TurnAbortedEvent:
		model.clearRetryStatus()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.finishDraft()
		model.finishTurn(model.runElapsed())
		model.running = false
		model.status = "aborted"
		model.restoreQueuedInputsToComposer()
	case protocol.ErrorEvent:
		model.clearRetryStatus()
		model.finishDraft()
		if !model.running {
			model.status = "idle"
		}
		if strings.TrimSpace(item.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Message))
		}
	case protocol.ShutdownCompleteEvent:
		model.status = "shutting down"
	}
	return nil
}
