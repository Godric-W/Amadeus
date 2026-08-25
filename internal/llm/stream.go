package llm

type Stream interface {
	Recv() (StreamChunk, error)
	Close() error
}

type StreamChunk struct {
	ID                   string
	RequestID            string
	ContentDelta         string
	ReasoningDelta       string
	FinishReason         FinishReason
	ProviderFinishReason string
	TokenUsage           *TokenUsage
	ToolCalls            []ToolCall
}

func (chunk StreamChunk) Completed() bool {
	return chunk.FinishReason != ""
}
