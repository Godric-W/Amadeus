package llm

import "encoding/json"

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Strict      bool
}

type ReasoningConfig struct {
	Enabled  *bool
	Preserve *bool
}

type Request struct {
	Model           string
	Messages        []Message
	Temperature     float64
	MaxOutputTokens int
	Tools           []ToolDefinition
	Reasoning       *ReasoningConfig
}

func NewRequest(model string, messages []Message) Request {
	return Request{
		Model:    model,
		Messages: cloneMessages(messages),
	}
}

func cloneMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].Parts = cloneContentParts(message.Parts)
		cloned[index].ToolCalls = cloneToolCalls(message.ToolCalls)
	}
	return cloned
}
