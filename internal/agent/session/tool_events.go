package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type toolEventObserver struct {
	appendItems   func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error
	turnID        protocol.TurnID
	threadID      protocol.ThreadID
	events        protocol.EventSink
	mu            sync.Mutex
	presentations map[string]toolCallPresentation
	collaboration map[string]protocol.CollabAgentToolCallItem
	resolveAgent  func(protocol.ThreadID) protocol.CollabAgentRef
}

type toolCallPresentation struct {
	actionSummary string
	detail        string
	sideEffect    tool.SideEffect
}

func (presentation toolCallPresentation) payload(duration time.Duration, partial bool) map[string]any {
	return map[string]any{
		"action_summary": presentation.actionSummary,
		"detail":         presentation.detail,
		"side_effect":    string(presentation.sideEffect),
		"duration":       duration.String(),
		"partial":        partial,
	}
}

func NewToolEventObserver(appendItems func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error, threadID protocol.ThreadID, turnID protocol.TurnID, events protocol.EventSink, resolveAgent func(protocol.ThreadID) protocol.CollabAgentRef) tool.LifecycleObserver {
	return &toolEventObserver{appendItems: appendItems, threadID: threadID, turnID: turnID, events: events, presentations: map[string]toolCallPresentation{}, collaboration: map[string]protocol.CollabAgentToolCallItem{}, resolveAgent: resolveAgent}
}

func (observer *toolEventObserver) ToolCallStarted(ctx context.Context, spec tool.ToolSpec, call tool.ToolCall) error {
	if !eventPolicyForTool(call.Name).emitActivity {
		return nil
	}
	presentation := presentToolCall(spec, call)
	if collabTool, ok := collabAgentTool(call.Name); ok {
		collaboration := collabAgentStarted(observer.threadID, collabTool, call, observer.resolveAgent)
		observer.mu.Lock()
		observer.collaboration[call.ID] = collaboration
		observer.mu.Unlock()
		item := protocol.TurnItem{ID: protocol.ItemID(call.ID), Kind: protocol.ItemCollabAgentToolCall, Status: protocol.ItemInProgress, CreatedAt: collaboration.CreatedAt, ToolName: call.Name, CallID: call.ID, CollabAgent: &collaboration}
		if err := observer.events.Publish(ctx, protocol.Event{Msg: protocol.ItemStartedEvent{Item: item}}); err != nil {
			observer.mu.Lock()
			delete(observer.collaboration, call.ID)
			observer.mu.Unlock()
			return err
		}
		return nil
	}
	snapshot := toolCallPresentation{actionSummary: presentation.ActionSummary, detail: presentation.Detail, sideEffect: spec.SideEffect}
	observer.mu.Lock()
	observer.presentations[call.ID] = snapshot
	observer.mu.Unlock()
	item := protocol.TurnItem{ID: protocol.ItemID(call.ID), Kind: toolItemKind(call.Name, spec.SideEffect), Status: protocol.ItemInProgress, CreatedAt: time.Now().UTC(), ToolName: call.Name, CallID: call.ID, Payload: snapshot.payload(0, false)}
	if err := observer.events.Publish(ctx, protocol.Event{Msg: protocol.ItemStartedEvent{Item: item}}); err != nil {
		observer.mu.Lock()
		delete(observer.presentations, call.ID)
		observer.mu.Unlock()
		return err
	}
	return nil
}

func (observer *toolEventObserver) ToolCallCompleted(ctx context.Context, execution tool.ToolExecution) error {
	if observer == nil || observer.appendItems == nil || observer.events == nil {
		return errors.New("tool event observer is incomplete")
	}
	result := execution.Output.Clone()
	displayResult := result.DisplaySafeClone()
	payload := rollout.ResponseItem{
		Type: rollout.ResponseToolResult, Role: string(llm.RoleTool), CallID: execution.Call.ID, Name: execution.Call.Name,
		Status: string(execution.Outcome.Status), Content: result.Text, Result: &displayResult,
		Metadata: execution.Outcome.Metadata, Partial: result.Partial, Duration: int64(execution.Outcome.Duration),
		Parts: append([]tool.ContentPart(nil), result.Parts...),
	}
	if execution.Outcome.Error != nil {
		payload.Error = &rollout.ResponseError{Kind: execution.Outcome.Error.Kind, Message: execution.Outcome.Error.Message}
	}
	responseItem, err := rollout.NewResponseItem(payload)
	if err != nil {
		return err
	}
	policy := eventPolicyForTool(execution.Call.Name)
	if !policy.emitActivity {
		completionCtx := context.WithoutCancel(ctx)
		if err := observer.appendItems(completionCtx, observer.turnID, responseItem); err != nil {
			return fmt.Errorf("persist tool completion: %w", err)
		}
		return nil
	}
	status := protocol.ItemStatusCompleted
	switch execution.Outcome.Status {
	case tool.ToolCallDenied:
		status = protocol.ItemDeclined
	case tool.ToolCallFailed, tool.ToolCallInterrupted:
		status = protocol.ItemFailed
	}
	now := time.Now().UTC()
	observer.mu.Lock()
	presentation := observer.presentations[execution.Call.ID]
	delete(observer.presentations, execution.Call.ID)
	collaboration, hasCollaboration := observer.collaboration[execution.Call.ID]
	delete(observer.collaboration, execution.Call.ID)
	observer.mu.Unlock()
	if hasCollaboration {
		collaboration = completeCollabAgentItem(collaboration, execution, now, status)
		turnItem := protocol.TurnItem{ID: protocol.ItemID(execution.Call.ID), Kind: protocol.ItemCollabAgentToolCall, Status: status, CreatedAt: collaboration.CreatedAt, CompletedAt: now, Text: toolExecutionSummary(execution), ToolName: execution.Call.Name, CallID: execution.Call.ID, ToolResult: &displayResult, CollabAgent: &collaboration}
		completedItem, err := rollout.NewEventMsgItem(protocol.ItemCompletedEvent{Item: turnItem})
		if err != nil {
			return err
		}
		completionCtx := context.WithoutCancel(ctx)
		if err := observer.appendItems(completionCtx, observer.turnID, responseItem, completedItem); err != nil {
			return fmt.Errorf("persist collaboration tool completion: %w", err)
		}
		return observer.events.Publish(completionCtx, protocol.Event{Msg: protocol.ItemCompletedEvent{Item: turnItem}})
	}
	itemPayload := presentation.payload(execution.Outcome.Duration, execution.Output.Partial)
	turnItem := protocol.TurnItem{ID: protocol.ItemID(execution.Call.ID), Kind: toolItemKind(execution.Call.Name, ""), Status: status, CreatedAt: now, CompletedAt: now, Text: toolExecutionSummary(execution), ToolName: execution.Call.Name, CallID: execution.Call.ID, ToolResult: &displayResult, Payload: itemPayload}
	completedItem, err := rollout.NewEventMsgItem(protocol.ItemCompletedEvent{Item: turnItem})
	if err != nil {
		return err
	}
	completionCtx := context.WithoutCancel(ctx)
	if err := observer.appendItems(completionCtx, observer.turnID, responseItem, completedItem); err != nil {
		return fmt.Errorf("persist tool completion: %w", err)
	}
	return observer.events.Publish(completionCtx, protocol.Event{Msg: protocol.ItemCompletedEvent{Item: turnItem}})
}

func collabAgentTool(name string) (protocol.CollabAgentTool, bool) {
	tool := protocol.CollabAgentTool(name)
	return tool, tool.Valid()
}

func collabAgentStarted(sender protocol.ThreadID, collaborationTool protocol.CollabAgentTool, call tool.ToolCall, resolveAgent func(protocol.ThreadID) protocol.CollabAgentRef) protocol.CollabAgentToolCallItem {
	item := protocol.CollabAgentToolCallItem{ID: protocol.ItemID(call.ID), Tool: collaborationTool, Status: protocol.CollabAgentToolInProgress, SenderThreadID: sender, CreatedAt: time.Now().UTC()}
	var arguments struct {
		ID      protocol.ThreadID   `json:"id"`
		IDs     []protocol.ThreadID `json:"ids"`
		Message string              `json:"message"`
	}
	_ = json.Unmarshal(call.Payload, &arguments)
	item.Prompt = boundCollaborationText(arguments.Message, 1000)
	if !arguments.ID.IsZero() {
		item.ReceiverAgents = []protocol.CollabAgentRef{resolveCollabAgentRef(arguments.ID, resolveAgent)}
	}
	for _, id := range arguments.IDs {
		item.ReceiverAgents = append(item.ReceiverAgents, resolveCollabAgentRef(id, resolveAgent))
	}
	return item
}

func resolveCollabAgentRef(id protocol.ThreadID, resolveAgent func(protocol.ThreadID) protocol.CollabAgentRef) protocol.CollabAgentRef {
	if resolveAgent == nil {
		return protocol.CollabAgentRef{ThreadID: id}
	}
	resolved := resolveAgent(id)
	if resolved.ThreadID.IsZero() {
		resolved.ThreadID = id
	}
	return resolved
}

func completeCollabAgentItem(item protocol.CollabAgentToolCallItem, execution tool.ToolExecution, completedAt time.Time, status protocol.ItemStatus) protocol.CollabAgentToolCallItem {
	item.CompletedAt = &completedAt
	item.Status = protocol.CollabAgentToolCompleted
	if status == protocol.ItemFailed || status == protocol.ItemDeclined {
		item.Status = protocol.CollabAgentToolFailed
	}
	encoded, _ := json.Marshal(execution.Output.Data)
	switch item.Tool {
	case protocol.CollabAgentSpawnAgent:
		var value struct {
			AgentID  protocol.ThreadID `json:"agent_id"`
			Nickname string            `json:"nickname"`
		}
		_ = json.Unmarshal(encoded, &value)
		if !value.AgentID.IsZero() {
			item.ReceiverAgents = []protocol.CollabAgentRef{{ThreadID: value.AgentID, AgentNickname: value.Nickname, AgentRole: "explorer"}}
		}
	case protocol.CollabAgentSendInput:
		var value struct {
			AgentID  protocol.ThreadID `json:"agent_id"`
			Nickname string            `json:"nickname"`
		}
		_ = json.Unmarshal(encoded, &value)
		if !value.AgentID.IsZero() {
			item.ReceiverAgents = []protocol.CollabAgentRef{{ThreadID: value.AgentID, AgentNickname: value.Nickname, AgentRole: "explorer"}}
		}
	case protocol.CollabAgentCloseAgent:
		var value struct {
			AgentID          protocol.ThreadID         `json:"agent_id"`
			Nickname         string                    `json:"nickname"`
			PreviousStatus   protocol.AgentStatus      `json:"previous_status"`
			PreviousLastTurn *protocol.AgentTurnResult `json:"previous_last_turn"`
		}
		_ = json.Unmarshal(encoded, &value)
		if !value.AgentID.IsZero() {
			item.ReceiverAgents = []protocol.CollabAgentRef{{ThreadID: value.AgentID, AgentNickname: value.Nickname, AgentRole: "explorer"}}
			item.AgentsStates = map[protocol.ThreadID]protocol.CollabAgentState{
				value.AgentID: {Status: value.PreviousStatus, LastTurn: value.PreviousLastTurn},
			}
		}
	case protocol.CollabAgentWait:
		var value struct {
			Statuses []struct {
				AgentID           protocol.ThreadID         `json:"agent_id"`
				Nickname          string                    `json:"nickname"`
				Role              string                    `json:"role"`
				Status            protocol.AgentStatus      `json:"status"`
				LastTurn          *protocol.AgentTurnResult `json:"last_turn"`
				NotificationError string                    `json:"notification_error"`
			} `json:"statuses"`
		}
		_ = json.Unmarshal(encoded, &value)
		item.ReceiverAgents = nil
		item.AgentsStates = make(map[protocol.ThreadID]protocol.CollabAgentState, len(value.Statuses))
		for _, status := range value.Statuses {
			item.ReceiverAgents = append(item.ReceiverAgents, protocol.CollabAgentRef{ThreadID: status.AgentID, AgentNickname: status.Nickname, AgentRole: status.Role})
			item.AgentsStates[status.AgentID] = protocol.CollabAgentState{Status: status.Status, LastTurn: status.LastTurn, NotificationError: status.NotificationError}
		}
	}
	return item
}

func boundCollaborationText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "…"
}

func toolItemKind(name string, effect tool.SideEffect) protocol.ItemKind {
	switch name {
	case "spawn_agent", "send_input", "wait_agent", "close_agent":
		return protocol.ItemCollabAgentToolCall
	}
	if name == "execute_command" || name == "write_stdin" {
		return protocol.ItemCommandExecution
	}
	if effect == tool.SideEffectWrite || name == "edit" || name == "write" {
		return protocol.ItemFileChange
	}
	return protocol.ItemToolCall
}

func toolExecutionSummary(execution tool.ToolExecution) string {
	if text := strings.TrimSpace(execution.Output.Text); text != "" {
		return text
	}
	if execution.Outcome.Error != nil && strings.TrimSpace(execution.Outcome.Error.Message) != "" {
		return execution.Outcome.Error.Message
	}
	if len(execution.Output.Parts) > 0 {
		return fmt.Sprintf("tool returned %d content part(s)", len(execution.Output.Parts))
	}
	if execution.Output.Partial {
		return "tool completed with partial output"
	}
	return "tool completed"
}
