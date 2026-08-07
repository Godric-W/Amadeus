package react

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ToolResultPayload struct {
	OK       bool               `json:"ok"`
	Text     string             `json:"text,omitempty"`
	Parts    []tool.ContentPart `json:"parts,omitempty"`
	Metadata map[string]any     `json:"metadata,omitempty"`
	Partial  bool               `json:"partial,omitempty"`
	Error    string             `json:"error,omitempty"`
}

func ReplayToolResults(assistant llm.Message, outcomes []ToolOutcome) ([]llm.Message, error) {
	if assistant.Role != llm.RoleAssistant {
		return nil, errors.New("tool result replay requires an assistant message")
	}
	if len(assistant.ToolCalls) == 0 {
		return nil, errors.New("tool result replay assistant message has no tool calls")
	}

	byCallID := make(map[string]ToolOutcome, len(outcomes))
	for index, outcome := range outcomes {
		callID := strings.TrimSpace(outcome.CallID)
		if callID == "" {
			return nil, fmt.Errorf("tool result replay executions[%d] has an empty call ID", index)
		}
		if _, exists := byCallID[callID]; exists {
			return nil, fmt.Errorf("tool result replay has duplicate result for call %q", callID)
		}
		byCallID[callID] = outcome
	}

	messages := make([]llm.Message, 0, 1+len(assistant.ToolCalls))
	messages = append(messages, assistant)
	seenCalls := make(map[string]struct{}, len(assistant.ToolCalls))
	for index, call := range assistant.ToolCalls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			return nil, fmt.Errorf("tool result replay assistant tool_calls[%d] has an empty call ID", index)
		}
		if _, exists := seenCalls[callID]; exists {
			return nil, fmt.Errorf("tool result replay assistant has duplicate call ID %q", callID)
		}
		seenCalls[callID] = struct{}{}
		outcome, exists := byCallID[callID]
		if !exists {
			return nil, fmt.Errorf("tool result replay is missing result for call %q", callID)
		}
		payload, err := encodeToolResultPayload(outcome)
		if err != nil {
			return nil, fmt.Errorf("tool result replay call %q: %w", callID, err)
		}
		parts := make([]llm.ContentPart, 0, len(outcome.Result.Parts))
		for _, part := range outcome.Result.Parts {
			switch part.Kind {
			case tool.ContentText:
				parts = append(parts, llm.TextPart(part.Text))
			case tool.ContentImage:
				parts = append(parts, llm.ImagePart(part.MediaType, part.Data))
			}
		}
		messages = append(messages, llm.ToolResultMessageWithParts(callID, payload, parts...))
	}
	if len(seenCalls) != len(byCallID) {
		for callID := range byCallID {
			if _, exists := seenCalls[callID]; !exists {
				return nil, fmt.Errorf("tool result replay has result for unknown call %q", callID)
			}
		}
	}
	return messages, nil
}

func encodeToolResultPayload(outcome ToolOutcome) (string, error) {
	result := outcome.Result.Clone()
	payload := ToolResultPayload{
		OK:       outcome.Succeeded(),
		Text:     result.Text,
		Metadata: result.Metadata,
		Partial:  result.Partial,
		Error:    outcome.ErrorMessage(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode tool result payload: %w", err)
	}
	return string(encoded), nil
}
