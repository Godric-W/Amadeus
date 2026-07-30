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

func ReplayToolResults(assistant llm.Message, executions []ToolExecution) ([]llm.Message, error) {
	if assistant.Role != llm.RoleAssistant {
		return nil, errors.New("tool result replay requires an assistant message")
	}
	if len(assistant.ToolCalls) == 0 {
		return nil, errors.New("tool result replay assistant message has no tool calls")
	}

	byCallID := make(map[string]ToolExecution, len(executions))
	for index, execution := range executions {
		callID := strings.TrimSpace(execution.Observation.CallID)
		if callID == "" {
			return nil, fmt.Errorf("tool result replay executions[%d] has an empty call ID", index)
		}
		if _, exists := byCallID[callID]; exists {
			return nil, fmt.Errorf("tool result replay has duplicate result for call %q", callID)
		}
		byCallID[callID] = execution
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
		execution, exists := byCallID[callID]
		if !exists {
			return nil, fmt.Errorf("tool result replay is missing result for call %q", callID)
		}
		payload, err := encodeToolResultPayload(execution)
		if err != nil {
			return nil, fmt.Errorf("tool result replay call %q: %w", callID, err)
		}
		messages = append(messages, llm.ToolResultMessage(callID, payload))
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

func encodeToolResultPayload(execution ToolExecution) (string, error) {
	result := execution.Observation.Result.Clone()
	payload := ToolResultPayload{
		OK:       execution.Observation.Error == "",
		Text:     result.Text,
		Parts:    result.Parts,
		Metadata: result.Metadata,
		Partial:  result.Partial,
		Error:    execution.Observation.Error,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode tool result payload: %w", err)
	}
	return string(encoded), nil
}
