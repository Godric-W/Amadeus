package contextmanager

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func BenchmarkPromptSnapshotLongHistory(b *testing.B) {
	manager := benchmarkContextManager(b, 1000)
	model := llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 128_000, ToolOutputTokenLimit: 2_000}
	prompt := llm.Prompt{BaseInstructions: llm.BaseInstructions{Text: "base instructions"}, Tools: []llm.ToolSpec{{Name: "read", Description: "read a file"}}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = manager.Snapshot(model, prompt)
	}
}

func BenchmarkPromptSnapshotWithTokenAccounting(b *testing.B) {
	manager := benchmarkContextManager(b, 1000)
	usage := rollout.EventMsgItem{Msg: protocol.TokenCountEvent{
		ThreadID: testutil.ThreadID(1), TurnID: "turn-benchmark",
		Info:                &protocol.TokenUsageInfo{TotalTokenUsage: llm.TokenUsage{TotalTokens: 20_000}, LastTokenUsage: llm.TokenUsage{TotalTokens: 500}},
		ActiveContextTokens: 20_000, ActiveContextEstimated: false, ObservedThroughSequence: manager.NextSequence() - 1,
	}}
	if err := manager.Record(manager.NextSequence(), usage); err != nil {
		b.Fatal(err)
	}
	model := llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 128_000}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = manager.ActiveContextTokens(model)
	}
}

func BenchmarkPromptPreviewRecordContinuation(b *testing.B) {
	manager := benchmarkContextManager(b, 1000)
	model := llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 128_000}
	prompt := llm.Prompt{}
	item := rollout.ResponseItem{ThreadID: testutil.ThreadID(1), TurnID: "turn-next", Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "continuation response"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = manager.PreviewRecord(manager.NextSequence(), model, prompt, item)
	}
}

func benchmarkContextManager(b *testing.B, count int) *Manager {
	b.Helper()
	manager := NewManager(nil)
	items := make([]rollout.RolloutItem, 0, count)
	for index := 0; index < count; index++ {
		kind := rollout.ResponseUserMessage
		role := "user"
		if index%2 == 1 {
			kind, role = rollout.ResponseAssistantMessage, "assistant"
		}
		items = append(items, rollout.ResponseItem{
			ThreadID: testutil.ThreadID(1), TurnID: protocol.TurnID("turn-benchmark"), Type: kind, Role: role,
			Content: "A representative long conversation item used for context accounting benchmarks.",
		})
	}
	if err := manager.Record(1, items...); err != nil {
		b.Fatal(err)
	}
	return manager
}
