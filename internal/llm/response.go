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

type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
	TotalTokens       int64
}

type Response struct {
	ID                   string
	RequestID            string
	Message              Message
	FinishReason         FinishReason
	ProviderFinishReason string
	Usage                Usage
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
