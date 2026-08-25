package llm

type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonCancelled     FinishReason = "cancelled"
	FinishReasonError         FinishReason = "error"
	FinishReasonUnknown       FinishReason = "unknown"
)

type TokenUsage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
	TotalTokens       int64
}

type Response struct {
	ID                   string
	RequestID            string
	Message              ResponseItem
	FinishReason         FinishReason
	ProviderFinishReason string
	TokenUsage           TokenUsage
}

func (usage *TokenUsage) Add(other TokenUsage) {
	if usage == nil {
		return
	}
	usage.InputTokens += other.InputTokens
	usage.CachedInputTokens += other.CachedInputTokens
	usage.OutputTokens += other.OutputTokens
	usage.ReasoningTokens += other.ReasoningTokens
	usage.TotalTokens += other.TotalTokens
}

func (reason FinishReason) Valid() bool {
	switch reason {
	case FinishReasonStop,
		FinishReasonLength,
		FinishReasonToolCalls,
		FinishReasonContentFilter,
		FinishReasonCancelled,
		FinishReasonError,
		FinishReasonUnknown:
		return true
	default:
		return false
	}
}
