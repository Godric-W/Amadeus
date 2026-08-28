package protocol

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestSessionSourceValidation(t *testing.T) {
	root := RootSessionSource()
	if err := root.Validate(); err != nil {
		t.Fatal(err)
	}
	subAgent := NewSubAgentSessionSource(testutil.ThreadID(1), 1, "atlas", "explorer")
	if err := subAgent.Validate(); err != nil {
		t.Fatal(err)
	}
	cloned := subAgent.Clone()
	cloned.SubAgent.AgentNickname = "changed"
	if subAgent.SubAgent.AgentNickname != "atlas" {
		t.Fatal("session source clone shared sub-agent metadata")
	}
}

func TestAgentStatusValidation(t *testing.T) {
	for _, status := range []AgentStatus{
		{Kind: AgentStatusPendingInit},
		{Kind: AgentStatusRunning},
		{Kind: AgentStatusInterrupted},
		{Kind: AgentStatusCompleted, Message: "done"},
		{Kind: AgentStatusErrored, Message: "failed"},
		{Kind: AgentStatusShutdown},
		{Kind: AgentStatusNotFound},
	} {
		if err := status.Validate(); err != nil {
			t.Fatalf("status %#v: %v", status, err)
		}
	}
	if err := (AgentStatus{Kind: AgentStatusRunning, Message: "invalid"}).Validate(); err == nil {
		t.Fatal("running status accepted a message")
	}
}

func TestAgentTurnResultValidationAndClone(t *testing.T) {
	message := "verified result"
	result := AgentTurnResult{TurnID: "turn-1", Outcome: TurnOutcomeBlocked, Reason: "budget reached", LastAgentMessage: &message}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	cloned := result.Clone()
	*cloned.LastAgentMessage = "changed"
	if *result.LastAgentMessage != "verified result" {
		t.Fatal("agent turn result clone shared final message")
	}
	if err := (AgentTurnResult{TurnID: "turn-1", Outcome: "future"}).Validate(); err == nil {
		t.Fatal("invalid agent turn outcome was accepted")
	}
}

func TestSubagentNotificationRequiresTerminalTurnIdentity(t *testing.T) {
	event := SubagentNotificationEvent{AgentID: testutil.ThreadID(2), TurnID: "turn-1", Content: "<subagent_notification>{}</subagent_notification>"}
	if _, err := EncodeEventMsg(event); err != nil {
		t.Fatal(err)
	}
	event.TurnID = ""
	if _, err := EncodeEventMsg(event); err == nil {
		t.Fatal("subagent notification without turn ID was accepted")
	}
}

func TestCollabAgentToolCallItemValidation(t *testing.T) {
	now := time.Now().UTC()
	item := CollabAgentToolCallItem{
		ID: "call-1", Tool: CollabAgentWait, Status: CollabAgentToolCompleted, SenderThreadID: testutil.ThreadID(1),
		ReceiverAgents: []CollabAgentRef{{ThreadID: testutil.ThreadID(2), AgentNickname: "atlas", AgentRole: "explorer"}},
		AgentsStates: map[ThreadID]CollabAgentState{testutil.ThreadID(2): {
			Status:   AgentStatus{Kind: AgentStatusCompleted, Message: "done"},
			LastTurn: &AgentTurnResult{TurnID: "turn-1", Outcome: TurnOutcomeCompleted, LastAgentMessage: stringPointer("done")},
		}},
		CreatedAt: now, CompletedAt: &now,
	}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string { return &value }

func TestTurnItemRejectsInconsistentCollaborationPayload(t *testing.T) {
	now := time.Now().UTC()
	completed := now.Add(time.Second)
	base := TurnItem{
		ID: "call-1", Kind: ItemCollabAgentToolCall, Status: ItemStatusCompleted,
		CreatedAt: now, CompletedAt: completed, ToolName: "wait_agent",
		CollabAgent: &CollabAgentToolCallItem{
			ID: "call-1", Tool: CollabAgentWait, Status: CollabAgentToolCompleted,
			SenderThreadID: testutil.ThreadID(1), CreatedAt: now, CompletedAt: &completed,
		},
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := base
	invalid.CollabAgent = cloneCollabAgentItem(*base.CollabAgent)
	invalid.CollabAgent.Status = CollabAgentToolInProgress
	invalid.CollabAgent.CompletedAt = nil
	if err := invalid.Validate(); err == nil {
		t.Fatal("accepted inconsistent collaboration status")
	}
}

func cloneCollabAgentItem(item CollabAgentToolCallItem) *CollabAgentToolCallItem {
	cloned := item
	return &cloned
}
