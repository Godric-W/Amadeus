package app

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestProjectRolloutItemsPreservesCanonicalSequenceWithoutResponseFallback(t *testing.T) {
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	assistant := protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "done"}
	toolItem := protocol.TurnItem{ID: "call-1", Kind: protocol.ItemToolCall, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "match", ToolName: "grep", CallID: "call-1"}
	lines := []rollout.Line{
		projectorLine(1, rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect"}),
		projectorLine(2, rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "done"}),
		projectorLine(3, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Item: assistant}}),
		projectorLine(4, rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseToolResult, Role: "tool", CallID: "call-1", Name: "grep", Status: "succeeded", Result: nil}),
		projectorLine(5, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Item: toolItem}}),
		projectorLine(7, rollout.EventMsgItem{Msg: protocol.ScopeEventMsg(protocol.NewTokenCountEvent(llm.TokenUsage{InputTokens: 10, TotalTokens: 10}, 128_000, 1), testutil.ThreadID(1), "turn-1")}),
		projectorLine(8, rollout.EventMsgItem{Msg: protocol.TokenCountEvent{ThreadID: testutil.ThreadID(1), TurnID: "turn-2", Info: &protocol.TokenUsageInfo{
			TotalTokenUsage: llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, LastTokenUsage: llm.TokenUsage{OutputTokens: 5, TotalTokens: 5}, ModelContextWindow: 128_000,
		}, ActiveContextTokens: 5}}),
		projectorLine(9, rollout.CompactedItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-2", Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn, Summary: "summary", ReplacementHistory: []llm.ResponseItem{llm.UserMessage("summary")}, ReplacementOrigins: []rollout.ReplacementOrigin{rollout.ReplacementOriginCompaction}, CoveredThroughSequence: 3, SourceHash: "hash"}),
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
	if projection.TokenInfo == nil || projection.TokenInfo.TotalTokenUsage.TotalTokens != 15 || projection.TokenInfo.LastTokenUsage.TotalTokens != 5 || projection.ActiveContextTokens != 5 {
		t.Fatalf("projected token snapshot = %#v", projection)
	}
}

func TestProjectRolloutItemsRejectsInvalidCompletedEvent(t *testing.T) {
	line := projectorLine(1, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{
		ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Item: protocol.TurnItem{Kind: protocol.ItemAssistantMessage},
	}})
	if _, err := ProjectRolloutItems([]rollout.Line{line}); err == nil {
		t.Fatal("invalid completed event was accepted")
	}
}

func TestProjectRolloutItemsPrefersCanonicalUserItem(t *testing.T) {
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	user := protocol.TurnItem{
		ID: "user-1", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted,
		CreatedAt: now, CompletedAt: now, Text: "inspect", ClientUserMessageID: "client-1",
	}
	lines := []rollout.Line{
		projectorLine(1, rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect"}),
		projectorLine(2, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Item: user}}),
	}
	projection, err := ProjectRolloutItems(lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Items) != 1 || projection.Items[0].ID != user.ID || projection.Items[0].ClientUserMessageID != "client-1" {
		t.Fatalf("projection = %#v", projection.Items)
	}
}

func projectorLine(sequence uint64, item rollout.RolloutItem) rollout.Line {
	return rollout.Line{Version: rollout.CurrentVersion, Sequence: sequence, Timestamp: time.Unix(int64(sequence), 0).UTC(), Item: item}
}
