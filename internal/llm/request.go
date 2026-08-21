package llm

import (
	"encoding/json"
	"strings"
)

type BaseInstructions struct {
	Text string
}

type OutputSchema json.RawMessage

type Prompt struct {
	Input              []ResponseItem
	Tools              []ToolSpec
	ParallelToolCalls  bool
	BaseInstructions   BaseInstructions
	OutputSchema       OutputSchema
	OutputSchemaStrict bool
}

type ToolSpec struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Strict      bool
}

type Request struct {
	Model     string
	Prompt    Prompt
	Reasoning *ReasoningConfig
}

func NewRequest(model string, messages []ResponseItem) Request {
	return Request{
		Model:  model,
		Prompt: Prompt{Input: cloneResponseItems(messages)},
	}
}

func (request Request) InputMessages() []ResponseItem {
	messages := make([]ResponseItem, 0, len(request.Prompt.Input)+1)
	if text := strings.TrimSpace(request.Prompt.BaseInstructions.Text); text != "" {
		messages = append(messages, SystemMessage(text))
	}
	messages = append(messages, cloneResponseItems(request.Prompt.Input)...)
	return messages
}

func (request Request) ToolSpecs() []ToolSpec {
	return cloneToolSpecs(request.Prompt.Tools)
}

func (request Request) RequestedOutputSchema() json.RawMessage {
	if len(request.Prompt.OutputSchema) > 0 {
		return append(json.RawMessage(nil), request.Prompt.OutputSchema...)
	}
	return nil
}

func cloneResponseItems(messages []ResponseItem) []ResponseItem {
	cloned := make([]ResponseItem, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].Parts = cloneContentParts(message.Parts)
		cloned[index].ToolCalls = cloneToolCalls(message.ToolCalls)
	}
	return cloned
}

func cloneToolSpecs(definitions []ToolSpec) []ToolSpec {
	cloned := make([]ToolSpec, len(definitions))
	for index, definition := range definitions {
		cloned[index] = definition
		cloned[index].InputSchema = append(json.RawMessage(nil), definition.InputSchema...)
	}
	return cloned
}
