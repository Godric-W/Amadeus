package multiagent

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type LifecycleState struct {
	Status   protocol.AgentStatus
	LastTurn *protocol.AgentTurnResult
}

func InitialLifecycleState() LifecycleState {
	return LifecycleState{Status: protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit}}
}

func ReduceLifecycleEvent(current LifecycleState, message protocol.EventMsg) (LifecycleState, bool, bool) {
	next := cloneLifecycleState(current)
	terminal := false
	switch event := message.(type) {
	case protocol.TurnStartedEvent:
		next.Status = protocol.AgentStatus{Kind: protocol.AgentStatusRunning}
		next.LastTurn = nil
	case protocol.TurnCompleteEvent:
		terminal = true
		outcome := event.Outcome
		if !outcome.Valid() {
			if event.Status == protocol.TurnStatusFailed || strings.TrimSpace(event.Error) != "" {
				outcome = protocol.TurnOutcomeFailed
			} else {
				outcome = protocol.TurnOutcomeCompleted
			}
		}
		result := protocol.AgentTurnResult{
			TurnID: event.TurnID, Outcome: outcome, Reason: boundReason(event.Reason),
			LastAgentMessage: cloneBoundedMessage(event.LastAgentMessage),
		}
		next.LastTurn = &result
		if event.Status == protocol.TurnStatusFailed || strings.TrimSpace(event.Error) != "" || outcome == protocol.TurnOutcomeFailed {
			text := strings.TrimSpace(event.Error)
			if text == "" {
				text = result.Reason
			}
			next.Status = protocol.AgentStatus{Kind: protocol.AgentStatusErrored, Message: boundMessage(text)}
		} else {
			message := ""
			if result.LastAgentMessage != nil {
				message = *result.LastAgentMessage
			}
			next.Status = protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, Message: message}
		}
	case protocol.TurnAbortedEvent:
		terminal = true
		result := protocol.AgentTurnResult{TurnID: event.TurnID, Outcome: protocol.TurnOutcomeAborted, Reason: boundReason(event.Reason)}
		next.Status = protocol.AgentStatus{Kind: protocol.AgentStatusInterrupted}
		next.LastTurn = &result
	case protocol.ErrorEvent:
		next.Status = protocol.AgentStatus{Kind: protocol.AgentStatusErrored, Message: boundMessage(event.Message)}
	case protocol.ShutdownCompleteEvent:
		terminal = true
		next.Status = protocol.AgentStatus{Kind: protocol.AgentStatusShutdown}
	default:
		return current, false, false
	}
	return next, !lifecycleStateEqual(current, next), terminal
}

func cloneLifecycleState(state LifecycleState) LifecycleState {
	if state.LastTurn != nil {
		lastTurn := state.LastTurn.Clone()
		state.LastTurn = &lastTurn
	}
	return state
}

func lifecycleStateEqual(left, right LifecycleState) bool {
	if left.Status != right.Status {
		return false
	}
	if left.LastTurn == nil || right.LastTurn == nil {
		return left.LastTurn == nil && right.LastTurn == nil
	}
	if left.LastTurn.TurnID != right.LastTurn.TurnID || left.LastTurn.Outcome != right.LastTurn.Outcome || left.LastTurn.Reason != right.LastTurn.Reason {
		return false
	}
	if left.LastTurn.LastAgentMessage == nil || right.LastTurn.LastAgentMessage == nil {
		return left.LastTurn.LastAgentMessage == nil && right.LastTurn.LastAgentMessage == nil
	}
	return *left.LastTurn.LastAgentMessage == *right.LastTurn.LastAgentMessage
}

func cloneBoundedMessage(message *string) *string {
	if message == nil {
		return nil
	}
	bounded := boundMessage(*message)
	if bounded == "" {
		return nil
	}
	return &bounded
}
