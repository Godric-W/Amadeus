package llm

import (
	"encoding/json"
	"errors"
	"strings"
)

type BaseInstructionsProvenanceType string

const (
	BaseInstructionsCustom BaseInstructionsProvenanceType = "custom"
	BaseInstructionsModel  BaseInstructionsProvenanceType = "model"
)

type BaseInstructionsProvenance struct {
	Type  BaseInstructionsProvenanceType `json:"type"`
	Model string                         `json:"model,omitempty"`
}

func (provenance BaseInstructionsProvenance) Validate() error {
	switch provenance.Type {
	case BaseInstructionsCustom:
		if strings.TrimSpace(provenance.Model) != "" {
			return errors.New("custom base instructions provenance cannot name a model")
		}
	case BaseInstructionsModel:
		if strings.TrimSpace(provenance.Model) == "" {
			return errors.New("model base instructions provenance has no model")
		}
	default:
		return errors.New("base instructions provenance is invalid")
	}
	return nil
}

type BaseInstructions struct {
	Text       string                     `json:"text"`
	Provenance BaseInstructionsProvenance `json:"provenance"`
}

func NewModelBaseInstructions(text, model string) BaseInstructions {
	return BaseInstructions{Text: strings.TrimSpace(text), Provenance: BaseInstructionsProvenance{Type: BaseInstructionsModel, Model: strings.TrimSpace(model)}}
}

func NewCustomBaseInstructions(text string) BaseInstructions {
	return BaseInstructions{Text: strings.TrimSpace(text), Provenance: BaseInstructionsProvenance{Type: BaseInstructionsCustom}}
}

func (instructions BaseInstructions) Clone() BaseInstructions { return instructions }

func (instructions BaseInstructions) Validate() error {
	if strings.TrimSpace(instructions.Text) == "" {
		return errors.New("base instructions are empty")
	}
	if instructions.Provenance.Type == "" {
		return nil
	}
	return instructions.Provenance.Validate()
}

func (instructions BaseInstructions) ValidatePersisted() error {
	if err := instructions.Validate(); err != nil {
		return err
	}
	return instructions.Provenance.Validate()
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
	Name         string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	Strict       bool
}

type Request struct {
	Model           string
	InputModalities []InputModality
	Prompt          Prompt
	Reasoning       *ReasoningConfig
	Metadata        RequestMetadata
}

func NewRequest(model string, messages []ResponseItem) Request {
	return Request{
		Model: model, InputModalities: []InputModality{InputModalityText},
		Prompt: Prompt{Input: cloneResponseItems(messages)},
	}
}

func (request Request) SupportsInput(modality InputModality) bool {
	return ModelInfo{InputModalities: request.InputModalities}.SupportsInput(modality)
}

func (request Request) ConversationItems() []ResponseItem {
	return cloneResponseItems(request.Prompt.Input)
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
		cloned[index].OutputSchema = append(json.RawMessage(nil), definition.OutputSchema...)
	}
	return cloned
}
