package llm

import (
	"encoding/json"
	"strings"
)

type BaseInstructions struct {
	Text string
}

type OutputSchema json.RawMessage

type ResponseItem = Message

type Prompt struct {
	BaseInstructions  BaseInstructions
	Input             []ResponseItem
	Tools             []ToolDefinition
	ParallelToolCalls bool
	OutputSchema      OutputSchema
}

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
	Prompt          Prompt
	Temperature     float64
	MaxOutputTokens int
	Reasoning       *ReasoningConfig
}

func NewRequest(model string, messages []Message) Request {
	return Request{
		Model:  model,
		Prompt: Prompt{Input: cloneMessages(messages)},
	}
}

func (request Request) InputMessages() []Message {
	messages := make([]Message, 0, len(request.Prompt.Input)+1)
	if text := strings.TrimSpace(request.Prompt.BaseInstructions.Text); text != "" {
		messages = append(messages, SystemMessage(text))
	}
	messages = append(messages, cloneMessages(request.Prompt.Input)...)
	return messages
}

func (request Request) ToolDefinitions() []ToolDefinition {
	return cloneToolDefinitions(request.Prompt.Tools)
}

func (request Request) RequestedOutputSchema() json.RawMessage {
	if len(request.Prompt.OutputSchema) > 0 {
		return append(json.RawMessage(nil), request.Prompt.OutputSchema...)
	}
	return nil
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

func cloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	cloned := make([]ToolDefinition, len(definitions))
	for index, definition := range definitions {
		cloned[index] = definition
		cloned[index].InputSchema = append(json.RawMessage(nil), definition.InputSchema...)
	}
	return cloned
}
