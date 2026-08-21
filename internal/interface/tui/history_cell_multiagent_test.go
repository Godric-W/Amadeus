package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func TestCollabAgentHistoryCellUsesTypedPayload(t *testing.T) {
	now := time.Now().UTC()
	item := protocol.TurnItem{
		ID: "call-1", Kind: protocol.ItemCollabAgentToolCall, Status: protocol.ItemStatusCompleted,
		CreatedAt: now, CompletedAt: now, ToolName: "wait_agent", Text: "not-json",
		CollabAgent: &protocol.CollabAgentToolCallItem{
			ID: "call-1", Tool: protocol.CollabAgentWait, Status: protocol.CollabAgentToolCompleted, SenderThreadID: "root",
			ReceiverAgents: []protocol.CollabAgentRef{{ThreadID: "child", AgentNickname: "atlas", AgentRole: "explorer"}},
			AgentsStates:   map[protocol.ThreadID]protocol.CollabAgentState{"child": {Status: protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, Message: "found ownership"}}},
			CreatedAt:      now, CompletedAt: &now,
		},
	}
	cell := newCollabAgentHistoryCell()
	if !cell.Apply(protocol.ItemCompletedEvent{Item: item}) {
		t.Fatal("typed collaboration item was not applied")
	}
	text := strings.Join(cell.RawLines(), "\n")
	if !strings.Contains(text, "Finished waiting") || !strings.Contains(text, "atlas: completed — found ownership") {
		t.Fatalf("rendered collaboration text = %q", text)
	}
}
