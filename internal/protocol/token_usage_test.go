package protocol

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestTokenUsageInfoAppendSeparatesTotalAndLast(t *testing.T) {
	info := TokenUsageInfo{}
	first := llm.TokenUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
	second := llm.TokenUsage{InputTokens: 20, CachedInputTokens: 5, OutputTokens: 3, TotalTokens: 23}
	info.Append(first, 128_000)
	info.Append(second, 128_000)
	if info.TotalTokenUsage != (llm.TokenUsage{InputTokens: 30, CachedInputTokens: 5, OutputTokens: 5, TotalTokens: 35}) {
		t.Fatalf("total usage = %#v", info.TotalTokenUsage)
	}
	if info.LastTokenUsage != second || info.ModelContextWindow != 128_000 {
		t.Fatalf("last usage info = %#v", info)
	}
}

func TestTokenCountEventRejectsNegativeSnapshot(t *testing.T) {
	event := TokenCountEvent{Info: &TokenUsageInfo{TotalTokenUsage: llm.TokenUsage{TotalTokens: -1}}}
	if err := event.Validate(); err == nil {
		t.Fatal("negative token usage was accepted")
	}
}
