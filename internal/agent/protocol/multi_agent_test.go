package protocol

import (
	"testing"
	"time"
)

func TestSessionSourceValidation(t *testing.T) {
	root := RootSessionSource()
	if err := root.Validate(); err != nil {
		t.Fatal(err)
	}
	subAgent := NewSubAgentSessionSource("parent", 1, "atlas", "explorer")
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

func TestCollabAgentToolCallItemValidation(t *testing.T) {
	now := time.Now().UTC()
	item := CollabAgentToolCallItem{
		ID: "call-1", Tool: CollabAgentWait, Status: CollabAgentToolCompleted, SenderThreadID: "root",
		ReceiverAgents: []CollabAgentRef{{ThreadID: "child", AgentNickname: "atlas", AgentRole: "explorer"}},
		AgentsStates:   map[ThreadID]CollabAgentState{"child": {Status: AgentStatus{Kind: AgentStatusCompleted, Message: "done"}}},
		CreatedAt:      now, CompletedAt: &now,
	}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTurnItemRejectsInconsistentCollaborationPayload(t *testing.T) {
	now := time.Now().UTC()
	completed := now.Add(time.Second)
	base := TurnItem{
		ID: "call-1", Kind: ItemCollabAgentToolCall, Status: ItemStatusCompleted,
		CreatedAt: now, CompletedAt: completed, ToolName: "wait_agent",
		CollabAgent: &CollabAgentToolCallItem{
			ID: "call-1", Tool: CollabAgentWait, Status: CollabAgentToolCompleted,
			SenderThreadID: "root", CreatedAt: now, CompletedAt: &completed,
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
