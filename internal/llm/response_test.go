package llm

import "testing"

func TestFinishReasonValidation(t *testing.T) {
	reasons := []FinishReason{
		FinishReasonStop,
		FinishReasonLength,
		FinishReasonToolCalls,
		FinishReasonContentFilter,
		FinishReasonCancelled,
		FinishReasonError,
		FinishReasonUnknown,
	}
	for _, reason := range reasons {
		if !reason.Valid() {
			t.Fatalf("known finish reason is invalid: %q", reason)
		}
	}
	if FinishReason("insufficient_system_resource").Valid() {
		t.Fatal("provider-specific finish reason is valid domain value")
	}
}

func TestResponsePreservesNormalizedAndProviderMetadata(t *testing.T) {
	response := Response{
		ID:                   "response-1",
		RequestID:            "request-1",
		Message:              Message{Role: RoleAssistant, Content: "done", Reasoning: "opaque reasoning"},
		FinishReason:         FinishReasonError,
		ProviderFinishReason: "insufficient_system_resource",
		Usage: Usage{
			InputTokens:       10,
			CachedInputTokens: 4,
			OutputTokens:      6,
			ReasoningTokens:   2,
			TotalTokens:       16,
		},
	}

	if response.FinishReason != FinishReasonError {
		t.Fatalf("unexpected normalized finish reason: %q", response.FinishReason)
	}
	if response.ProviderFinishReason != "insufficient_system_resource" {
		t.Fatalf("unexpected provider finish reason: %q", response.ProviderFinishReason)
	}
	if response.Usage.CachedInputTokens != 4 || response.Usage.ReasoningTokens != 2 {
		t.Fatalf("unexpected usage: %#v", response.Usage)
	}
}
