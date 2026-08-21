package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) rejectTurn(submissionID protocol.SubmissionID, turnID protocol.TurnID, err error, fatal bool) {
	if err == nil {
		err = errors.New("turn was rejected")
	}
	event := protocol.ErrorEvent{
		ThreadID: session.threadID,
		TurnID:   turnID,
		Code:     "turn_start_failed",
		Message:  err.Error(),
		At:       session.services.Clock().UTC(),
	}
	session.publish(protocol.Event{ID: submissionID, Msg: event})
	if fatal {
		session.cancel(fmt.Errorf("start turn persistence: %w", err))
	}
}

func (session *Session) finishTurn(completion Completion) {
	if session.active == nil || session.active.Task.Context().TurnID != completion.TurnID {
		return
	}
	submissionID := session.active.SubmissionID
	session.active.Output = mergeTaskOutput(session.active.Output, completion.Output)
	completion.Output = session.active.Output
	if completion.Cause == nil && completion.Error == nil && session.active.Task.Kind() == TaskKindRegular {
		if !session.inputQueue.SealIfEmpty(session.active.State) {
			if err := session.restartActiveTurn(); err == nil {
				return
			} else {
				completion.Error = err
				if recordErr := session.recordPendingInputBeforeTerminal(completion.TurnID); recordErr != nil {
					completion.Error = errors.Join(completion.Error, recordErr)
				}
			}
		}
	} else {
		if err := session.recordPendingInputBeforeTerminal(completion.TurnID); err != nil && completion.Error == nil {
			completion.Error = err
		}
	}
	if err := session.persistTaskOutput(submissionID, &completion); err != nil && completion.Error == nil {
		completion.Error = err
	}
	if completion.Cause != nil {
		session.finishAbortedTurn(submissionID, completion)
		return
	}
	session.finishCompletedTurn(submissionID, completion)
}

func (session *Session) restartActiveTurn() error {
	if session.active == nil || session.active.Task == nil {
		return errors.New("active turn is unavailable for continuation")
	}
	previous := session.active.Task
	running, err := NewRunningTask(session.ctx, session, previous.task, previous.context)
	if err != nil {
		return fmt.Errorf("restart active turn continuation: %w", err)
	}
	session.active.Task = running
	session.watchRunningTask(running)
	return nil
}

func (session *Session) recordPendingInputBeforeTerminal(turnID protocol.TurnID) error {
	if session.active == nil || session.active.State == nil {
		return nil
	}
	pending := session.inputQueue.SealAndDrain(session.active.State)
	if len(pending) == 0 {
		return nil
	}
	regular, _ := session.active.Task.task.(*regularTask)
	var events protocol.EventSink
	if regular != nil {
		events = regular.events
	}
	cleanupCtx, cancel := session.cleanupContext()
	defer cancel()
	for _, input := range pending {
		userInput, ok := input.(UserTurnInput)
		if !ok {
			return fmt.Errorf("unsupported terminal turn input %T", input)
		}
		if err := session.recordUserTurnInput(cleanupCtx, turnID, events, userInput); err != nil {
			return err
		}
	}
	return nil
}

func mergeTaskOutput(total, next TaskOutput) TaskOutput {
	total.Items = append(total.Items, next.Items...)
	total.Usage = addUsage(total.Usage, next.Usage)
	total.ToolCallCount += next.ToolCallCount
	if next.Summary != "" {
		total.Summary = next.Summary
	}
	if next.Outcome != "" {
		total.Outcome = next.Outcome
	}
	if next.Reason != "" {
		total.Reason = next.Reason
	}
	return total
}

func (session *Session) persistTaskOutput(submissionID protocol.SubmissionID, completion *Completion) error {
	items := append([]rollout.RolloutItem(nil), completion.Output.Items...)
	if completion.Output.Usage.TotalTokens > 0 {
		usageItem, err := engine.UsageItem(completion.Output.Usage)
		if err != nil {
			return err
		}
		items = append(items, usageItem)
	}
	if len(items) == 0 {
		return nil
	}
	cleanupCtx, cancel := session.cleanupContext()
	err := session.appendItemsDurable(cleanupCtx, completion.TurnID, items...)
	cancel()
	if err == nil {
		session.publishCompactionEvents(submissionID, completion.TurnID, items)
	}
	return err
}

func (session *Session) finishAbortedTurn(submissionID protocol.SubmissionID, completion Completion) {
	summary := strings.TrimSpace(completion.Output.Summary)
	if summary == "" {
		summary = "result: cancelled"
	}
	event := protocol.TurnAbortedEvent{
		ThreadID:   session.threadID,
		TurnID:     completion.TurnID,
		Summary:    summary,
		Reason:     completion.Cause.Error(),
		FinishedAt: session.services.Clock().UTC(),
	}
	persistErr := session.persistTerminal(completion.TurnID, event)
	session.clearActiveTurn()
	if persistErr != nil {
		session.failTerminalPersistence(submissionID, completion.TurnID, "aborted", persistErr)
		return
	}
	session.publish(protocol.Event{ID: submissionID, Msg: event})
}

func (session *Session) finishCompletedTurn(submissionID protocol.SubmissionID, completion Completion) {
	status, outcome, reason, summary, taskErr := normalizeTaskCompletion(completion.Output, completion.Error)
	finishedAt := session.services.Clock().UTC()
	events := make([]protocol.EventMsg, 0, 2)
	if taskErr != nil {
		events = append(events, protocol.ErrorEvent{
			ThreadID: session.threadID,
			TurnID:   completion.TurnID,
			Code:     "turn_failed",
			Message:  taskErr.Error(),
			At:       finishedAt,
		})
	}
	events = append(events, protocol.TurnCompleteEvent{
		ThreadID:   session.threadID,
		TurnID:     completion.TurnID,
		Status:     status,
		Outcome:    outcome,
		Reason:     reason,
		Summary:    summary,
		Error:      errorText(taskErr),
		FinishedAt: finishedAt,
	})
	persistErr := session.persistTerminal(completion.TurnID, events...)
	session.clearActiveTurn()
	if persistErr != nil {
		session.failTerminalPersistence(submissionID, completion.TurnID, "completed", persistErr)
		return
	}
	for _, event := range events {
		session.publish(protocol.Event{ID: submissionID, Msg: event})
	}
}

func normalizeTaskCompletion(output TaskOutput, taskErr error) (protocol.TurnTerminalStatus, protocol.TurnOutcome, string, string, error) {
	if taskErr == nil {
		switch output.Outcome {
		case "", protocol.TurnOutcomeCompleted:
			output.Outcome = protocol.TurnOutcomeCompleted
		case protocol.TurnOutcomeBlocked:
		default:
			taskErr = fmt.Errorf("task returned unsupported outcome %q", output.Outcome)
		}
	}
	reason := strings.TrimSpace(output.Reason)
	summary := strings.TrimSpace(output.Summary)
	if taskErr != nil {
		if reason == "" {
			reason = taskErr.Error()
		}
		if summary == "" {
			summary = "result: failed"
		}
		return protocol.TurnStatusFailed, protocol.TurnOutcomeFailed, reason, summary, taskErr
	}
	if summary == "" {
		summary = "result: " + string(output.Outcome)
	}
	return protocol.TurnStatusCompleted, output.Outcome, reason, summary, nil
}

func (session *Session) completeWithoutTask(submissionID protocol.SubmissionID, turnID protocol.TurnID, taskErr error) {
	if taskErr == nil {
		taskErr = errors.New("session task could not start")
	}
	finishedAt := session.services.Clock().UTC()
	errorEvent := protocol.ErrorEvent{
		ThreadID: session.threadID,
		TurnID:   turnID,
		Code:     "turn_failed",
		Message:  taskErr.Error(),
		At:       finishedAt,
	}
	completedEvent := protocol.TurnCompleteEvent{
		ThreadID:   session.threadID,
		TurnID:     turnID,
		Status:     protocol.TurnStatusFailed,
		Outcome:    protocol.TurnOutcomeFailed,
		Reason:     taskErr.Error(),
		Summary:    "result: failed",
		Error:      taskErr.Error(),
		FinishedAt: finishedAt,
	}
	if err := session.persistTerminal(turnID, errorEvent, completedEvent); err != nil {
		session.failTerminalPersistence(submissionID, turnID, "failed", err)
		return
	}
	session.publish(protocol.Event{ID: submissionID, Msg: errorEvent})
	session.publish(protocol.Event{ID: submissionID, Msg: completedEvent})
}

func (session *Session) persistTerminal(turnID protocol.TurnID, events ...protocol.EventMsg) error {
	items := make([]rollout.RolloutItem, len(events))
	for index, event := range events {
		items[index] = rollout.EventMsgItem{Msg: event}
	}
	cleanupCtx, cancel := session.cleanupContext()
	err := session.appendItemsDurable(cleanupCtx, turnID, items...)
	cancel()
	return err
}

func (session *Session) clearActiveTurn() {
	session.clearPendingRequests()
	if session.active != nil {
		session.inputQueue.SealAndDrain(session.active.State)
	}
	session.active = nil
}

func (session *Session) failTerminalPersistence(submissionID protocol.SubmissionID, turnID protocol.TurnID, terminal string, err error) {
	session.publish(protocol.Event{ID: submissionID, Msg: protocol.StreamErrorEvent{
		ThreadID: session.threadID,
		TurnID:   turnID,
		Message:  err.Error(),
	}})
	session.cancel(fmt.Errorf("persist %s turn: %w", terminal, err))
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
