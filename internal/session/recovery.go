package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func PendingToolCalls(items []RolloutItem, runID RunID) ([]ToolCallRecord, error) {
	pending := make([]ToolCallRecord, 0)
	positions := make(map[string]int)
	resolved := make(map[string]struct{})
	for index, item := range items {
		if item.RunID != runID {
			continue
		}
		switch item.Kind {
		case RolloutToolCall:
			payload, err := DecodeToolCall(item)
			if err != nil {
				return nil, fmt.Errorf("pending Tool Calls item %d: %w", index, err)
			}
			for _, call := range payload.Calls {
				if _, exists := positions[call.ID]; exists {
					return nil, fmt.Errorf("pending Tool Calls contain duplicate call ID %q", call.ID)
				}
				positions[call.ID] = len(pending)
				pending = append(pending, call)
			}
		case RolloutToolResult:
			payload, err := DecodeToolResult(item)
			if err != nil {
				return nil, fmt.Errorf("pending Tool Results item %d: %w", index, err)
			}
			if _, exists := positions[payload.CallID]; !exists {
				return nil, fmt.Errorf("Tool Result %q has no preceding Tool Call in Run %q", payload.CallID, runID)
			}
			if _, duplicate := resolved[payload.CallID]; duplicate {
				return nil, fmt.Errorf("Tool Call %q has multiple results", payload.CallID)
			}
			resolved[payload.CallID] = struct{}{}
		}
	}
	result := make([]ToolCallRecord, 0, len(pending)-len(resolved))
	for _, call := range pending {
		if _, ok := resolved[call.ID]; !ok {
			result = append(result, call)
		}
	}
	return result, nil
}

func InterruptedToolResultDraft(id RolloutItemID, runID RunID, call ToolCallRecord, at time.Time, reason, errorKind string) (AppendItem, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Tool execution was interrupted before a result was recorded."
	}
	errorKind = strings.TrimSpace(errorKind)
	if errorKind == "" {
		errorKind = "interrupted"
	}
	payload, err := EncodePayload(ToolResultPayload{
		CallID: call.ID, ToolName: call.Name, Status: "interrupted",
		Error: &ToolErrorPayload{Kind: errorKind, Message: reason},
	})
	if err != nil {
		return AppendItem{}, err
	}
	return AppendItem{ID: id, RunID: runID, Kind: RolloutToolResult, Payload: payload, CreatedAt: at.UTC()}, nil
}

func RecoveryItemID(runID RunID, callID string) RolloutItemID {
	digest := sha256.Sum256([]byte(strings.TrimSpace(callID)))
	return RolloutItemID("recovered-" + string(runID) + "-tool-" + hex.EncodeToString(digest[:8]))
}
