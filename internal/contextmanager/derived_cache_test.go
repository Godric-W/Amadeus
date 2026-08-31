package contextmanager

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestPromptDerivedCacheClonesAndInvalidatesOnRecord(t *testing.T) {
	manager := NewManager(nil)
	first := rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseUserMessage, Role: "user", Content: "first"}
	if err := manager.Record(1, first); err != nil {
		t.Fatal(err)
	}
	model := llm.ModelInfo{ContextWindow: 10_000}
	firstSnapshot := manager.Snapshot(model, llm.Prompt{})
	if manager.snapshotCache == nil {
		t.Fatal("snapshot was not cached")
	}
	firstSnapshot.Items[0].Content = "mutated caller copy"
	secondSnapshot := manager.Snapshot(model, llm.Prompt{})
	if secondSnapshot.Items[0].Content != "first" {
		t.Fatalf("cached snapshot leaked mutable item: %#v", secondSnapshot.Items)
	}
	second := rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "second"}
	if err := manager.Record(2, second); err != nil {
		t.Fatal(err)
	}
	if manager.snapshotCache != nil {
		t.Fatal("record did not invalidate snapshot cache")
	}
	updated := manager.Snapshot(model, llm.Prompt{})
	if len(updated.Items) != 2 || updated.Items[1].Content != "second" {
		t.Fatalf("snapshot after invalidation = %#v", updated.Items)
	}
}

func TestActiveTokenDerivedCacheInvalidatesWithTokenWatermark(t *testing.T) {
	manager := NewManager(nil)
	item := rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Type: rollout.ResponseUserMessage, Role: "user", Content: "message"}
	if err := manager.Record(1, item); err != nil {
		t.Fatal(err)
	}
	model := llm.ModelInfo{ContextWindow: 10_000}
	usage := rollout.EventMsgItem{Msg: protocol.TokenCountEvent{
		ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Info: &protocol.TokenUsageInfo{TotalTokenUsage: llm.TokenUsage{TotalTokens: 10}, LastTokenUsage: llm.TokenUsage{TotalTokens: 10}},
		ActiveContextTokens: 10, ObservedThroughSequence: 1,
	}}
	if err := manager.Record(2, usage); err != nil {
		t.Fatal(err)
	}
	if active, _ := manager.ActiveContextTokens(model); active != 10 {
		t.Fatalf("initial active token estimate = %d", active)
	}
	if manager.activeTokenCache == nil {
		t.Fatal("active token estimate was not cached")
	}
	usage = rollout.EventMsgItem{Msg: protocol.TokenCountEvent{
		ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Info: &protocol.TokenUsageInfo{TotalTokenUsage: llm.TokenUsage{TotalTokens: 20}, LastTokenUsage: llm.TokenUsage{TotalTokens: 10}},
		ActiveContextTokens: 20, ObservedThroughSequence: 2,
	}}
	if err := manager.Record(3, usage); err != nil {
		t.Fatal(err)
	}
	if manager.activeTokenCache != nil {
		t.Fatal("token record did not invalidate active cache")
	}
	if active, _ := manager.ActiveContextTokens(model); active != 20 {
		t.Fatalf("updated active token estimate = %d", active)
	}
}
