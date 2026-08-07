package session

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type UserMessagePayload struct {
	Content string `json:"content"`
}

type AssistantMessagePayload struct {
	ResponseID string `json:"response_id,omitempty"`
	Content    string `json:"content"`
}

type ToolCallRecord struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolCallPayload struct {
	ResponseID string           `json:"response_id,omitempty"`
	Content    string           `json:"content,omitempty"`
	Calls      []ToolCallRecord `json:"calls"`
}

type ToolContentPart struct {
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type ToolErrorPayload struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type ToolResultPayload struct {
	CallID   string            `json:"call_id"`
	ToolName string            `json:"tool_name"`
	Status   string            `json:"status"`
	Text     string            `json:"text,omitempty"`
	Parts    []ToolContentPart `json:"parts,omitempty"`
	Metadata map[string]any    `json:"metadata,omitempty"`
	Error    *ToolErrorPayload `json:"error,omitempty"`
	Partial  bool              `json:"partial,omitempty"`
	Duration int64             `json:"duration_nanos,omitempty"`
}

type RunMarkerPayload struct {
	Reason      string   `json:"reason"`
	ErrorKind   string   `json:"error_kind,omitempty"`
	Guidance    string   `json:"guidance,omitempty"`
	ActiveCalls []string `json:"active_calls,omitempty"`
}

type CompactionHistoryItem struct {
	Role    llm.Role `json:"role"`
	Content string   `json:"content"`
}

type ContextCompactionPayload struct {
	Summary                string                  `json:"summary"`
	ReplacementHistory     []CompactionHistoryItem `json:"replacement_history"`
	CoveredThroughSequence int64                   `json:"covered_through_sequence"`
	SourceHash             string                  `json:"source_hash"`
	Provider               string                  `json:"provider,omitempty"`
	Model                  string                  `json:"model,omitempty"`
}

type PlanUpdateItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type PlanUpdatePayload struct {
	Explanation string           `json:"explanation,omitempty"`
	Items       []PlanUpdateItem `json:"items"`
	UpdatedAt   string           `json:"updated_at"`
	Revision    int64            `json:"revision"`
}

func EncodePayload(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if string(encoded) == "null" {
		return nil, errors.New("rollout payload cannot be null")
	}
	return encoded, nil
}

func DecodeUserMessage(item RolloutItem) (UserMessagePayload, error) {
	if item.Kind != RolloutUserMessage {
		return UserMessagePayload{}, errors.New("rollout item is not a user message")
	}
	var payload UserMessagePayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return UserMessagePayload{}, err
	}
	payload.Content = strings.TrimSpace(payload.Content)
	if payload.Content == "" {
		return UserMessagePayload{}, errors.New("user message content is empty")
	}
	return payload, nil
}

func DecodeAssistantMessage(item RolloutItem) (AssistantMessagePayload, error) {
	if item.Kind != RolloutAssistantMessage {
		return AssistantMessagePayload{}, errors.New("rollout item is not an assistant message")
	}
	var payload AssistantMessagePayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return AssistantMessagePayload{}, err
	}
	payload.Content = strings.TrimSpace(payload.Content)
	if payload.Content == "" {
		return AssistantMessagePayload{}, errors.New("assistant message content is empty")
	}
	return payload, nil
}

func DecodeToolCall(item RolloutItem) (ToolCallPayload, error) {
	if item.Kind != RolloutToolCall {
		return ToolCallPayload{}, errors.New("rollout item is not a tool call")
	}
	var payload ToolCallPayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return ToolCallPayload{}, err
	}
	if len(payload.Calls) == 0 {
		return ToolCallPayload{}, errors.New("tool call payload has no calls")
	}
	seen := make(map[string]struct{}, len(payload.Calls))
	for index := range payload.Calls {
		payload.Calls[index].ID = strings.TrimSpace(payload.Calls[index].ID)
		payload.Calls[index].Name = strings.TrimSpace(payload.Calls[index].Name)
		if payload.Calls[index].ID == "" || payload.Calls[index].Name == "" || len(payload.Calls[index].Arguments) == 0 {
			return ToolCallPayload{}, errors.New("tool call payload contains an incomplete call")
		}
		if _, duplicate := seen[payload.Calls[index].ID]; duplicate {
			return ToolCallPayload{}, errors.New("tool call payload contains duplicate call IDs")
		}
		seen[payload.Calls[index].ID] = struct{}{}
	}
	return payload, nil
}

func DecodeToolResult(item RolloutItem) (ToolResultPayload, error) {
	if item.Kind != RolloutToolResult {
		return ToolResultPayload{}, errors.New("rollout item is not a tool result")
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return ToolResultPayload{}, err
	}
	payload.CallID = strings.TrimSpace(payload.CallID)
	payload.ToolName = strings.TrimSpace(payload.ToolName)
	payload.Status = strings.TrimSpace(payload.Status)
	if payload.CallID == "" || payload.ToolName == "" || payload.Status == "" {
		return ToolResultPayload{}, errors.New("tool result payload is incomplete")
	}
	return payload, nil
}

func DecodeRunMarker(item RolloutItem) (RunMarkerPayload, error) {
	if item.Kind != RolloutRunInterrupted && item.Kind != RolloutRunFailed {
		return RunMarkerPayload{}, errors.New("rollout item is not a Run marker")
	}
	var payload RunMarkerPayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return RunMarkerPayload{}, err
	}
	payload.Reason = strings.TrimSpace(payload.Reason)
	payload.ErrorKind = strings.TrimSpace(payload.ErrorKind)
	payload.Guidance = strings.TrimSpace(payload.Guidance)
	if payload.Reason == "" {
		return RunMarkerPayload{}, errors.New("Run marker reason is empty")
	}
	return payload, nil
}

func DecodeContextCompaction(item RolloutItem) (ContextCompactionPayload, error) {
	if item.Kind != RolloutContextCompaction {
		return ContextCompactionPayload{}, errors.New("rollout item is not a context compaction")
	}
	var payload ContextCompactionPayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return ContextCompactionPayload{}, err
	}
	payload.Summary = strings.TrimSpace(payload.Summary)
	payload.SourceHash = strings.ToLower(strings.TrimSpace(payload.SourceHash))
	payload.Provider = strings.TrimSpace(payload.Provider)
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Summary == "" {
		return ContextCompactionPayload{}, errors.New("context compaction summary is empty")
	}
	if payload.CoveredThroughSequence < 1 || payload.CoveredThroughSequence >= item.Sequence {
		return ContextCompactionPayload{}, errors.New("context compaction covered sequence is invalid")
	}
	if len(payload.SourceHash) != sha256.Size*2 {
		return ContextCompactionPayload{}, errors.New("context compaction source hash is invalid")
	}
	for _, character := range payload.SourceHash {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return ContextCompactionPayload{}, errors.New("context compaction source hash is invalid")
		}
	}
	if len(payload.ReplacementHistory) == 0 {
		return ContextCompactionPayload{}, errors.New("context compaction replacement history is empty")
	}
	for index := range payload.ReplacementHistory {
		payload.ReplacementHistory[index].Content = strings.TrimSpace(payload.ReplacementHistory[index].Content)
		if !payload.ReplacementHistory[index].Role.Valid() || payload.ReplacementHistory[index].Role == llm.RoleSystem || payload.ReplacementHistory[index].Role == llm.RoleTool || payload.ReplacementHistory[index].Content == "" {
			return ContextCompactionPayload{}, fmt.Errorf("context compaction replacement history item %d is invalid", index)
		}
	}
	return payload, nil
}

func DecodePlanUpdate(item RolloutItem) (PlanUpdatePayload, error) {
	if item.Kind != RolloutPlanUpdate {
		return PlanUpdatePayload{}, errors.New("rollout item is not a plan update")
	}
	var payload PlanUpdatePayload
	if err := json.Unmarshal(item.PayloadJSON, &payload); err != nil {
		return PlanUpdatePayload{}, err
	}
	payload.Explanation = strings.TrimSpace(payload.Explanation)
	payload.UpdatedAt = strings.TrimSpace(payload.UpdatedAt)
	if payload.Revision < 1 || payload.UpdatedAt == "" || len(payload.Items) == 0 {
		return PlanUpdatePayload{}, errors.New("plan update payload is incomplete")
	}
	for index := range payload.Items {
		payload.Items[index].Step = strings.TrimSpace(payload.Items[index].Step)
		payload.Items[index].Status = strings.TrimSpace(payload.Items[index].Status)
		if payload.Items[index].Step == "" {
			return PlanUpdatePayload{}, fmt.Errorf("plan update item %d is empty", index)
		}
		switch payload.Items[index].Status {
		case "pending", "in_progress", "completed":
		default:
			return PlanUpdatePayload{}, fmt.Errorf("plan update item %d status is invalid", index)
		}
	}
	return payload, nil
}
