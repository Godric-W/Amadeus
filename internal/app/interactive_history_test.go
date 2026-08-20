package app

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func TestProjectRolloutItemsPreservesCanonicalSequenceWithoutResponseFallback(t *testing.T) {
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	assistant := protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "done"}
	toolItem := protocol.TurnItem{ID: "call-1", Kind: protocol.ItemToolCall, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "match", ToolName: "grep", CallID: "call-1"}
	lines := []rollout.Line{
		projectorLine(1, rollout.ResponseItem{ThreadID: "thread-1", TurnID: "turn-1", Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect"}),
		projectorLine(2, rollout.ResponseItem{ThreadID: "thread-1", TurnID: "turn-1", Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "done"}),
		projectorLine(3, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{ThreadID: "thread-1", TurnID: "turn-1", Item: assistant}}),
		projectorLine(4, rollout.ResponseItem{ThreadID: "thread-1", TurnID: "turn-1", Type: rollout.ResponseToolResult, Role: "tool", CallID: "call-1", Name: "grep", Status: "succeeded", Result: nil}),
		projectorLine(5, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{ThreadID: "thread-1", TurnID: "turn-1", Item: toolItem}}),
		projectorLine(7, rollout.EventMsgItem{Msg: protocol.TokenCountEvent{ThreadID: "thread-1", TurnID: "turn-1", Usage: llm.Usage{InputTokens: 10, TotalTokens: 10}}}),
		projectorLine(8, rollout.EventMsgItem{Msg: protocol.TokenCountEvent{ThreadID: "thread-1", TurnID: "turn-2", Usage: llm.Usage{OutputTokens: 5, TotalTokens: 5}}}),
		projectorLine(9, rollout.CompactedItem{ThreadID: "thread-1", TurnID: "turn-2", Summary: "summary", ReplacementHistory: []rollout.ReplacementMessage{{Role: "assistant", Content: "summary"}}, CoveredThroughSequence: 3, SourceHash: "hash"}),
	}
	projection, err := ProjectRolloutItems(lines)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []protocol.ItemKind{protocol.ItemUserMessage, protocol.ItemAssistantMessage, protocol.ItemToolCall, protocol.ItemContextCompaction}
	if len(projection.Items) != len(wantKinds) {
		t.Fatalf("projected items = %#v", projection.Items)
	}
	for index, kind := range wantKinds {
		if projection.Items[index].Kind != kind {
			t.Fatalf("item %d kind = %q, want %q", index, projection.Items[index].Kind, kind)
		}
	}
	if projection.Usage.InputTokens != 10 || projection.Usage.OutputTokens != 5 || projection.Usage.TotalTokens != 15 {
		t.Fatalf("projected usage = %#v", projection.Usage)
	}
}

func TestProjectRolloutItemsRejectsInvalidCompletedEvent(t *testing.T) {
	line := projectorLine(1, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{
		ThreadID: "thread-1", TurnID: "turn-1", Item: protocol.TurnItem{Kind: protocol.ItemAssistantMessage},
	}})
	if _, err := ProjectRolloutItems([]rollout.Line{line}); err == nil {
		t.Fatal("invalid completed event was accepted")
	}
}

func projectorLine(sequence uint64, item rollout.RolloutItem) rollout.Line {
	return rollout.Line{Version: rollout.CurrentVersion, Sequence: sequence, Timestamp: time.Unix(int64(sequence), 0).UTC(), Item: item}
}
