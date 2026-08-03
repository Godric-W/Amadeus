package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const MaxInterruptedContextBytes = 256 << 10

const (
	maxInterruptedItems      = 64
	maxInterruptedTextRunes  = 1024
	maxInterruptedUsageBytes = 16 << 10
)

type InterruptedContextV1 struct {
	Type           string          `json:"type"`
	Objective      string          `json:"objective"`
	Status         string          `json:"status"`
	StopReason     string          `json:"stop_reason,omitempty"`
	CompletedSteps []string        `json:"completed_steps,omitempty"`
	Evidence       []string        `json:"evidence,omitempty"`
	RelevantPaths  []string        `json:"relevant_paths,omitempty"`
	PendingWork    []string        `json:"pending_work,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
}

func EncodeInterruptedContext(context InterruptedContextV1) (json.RawMessage, error) {
	context.Type = "amadeus.interrupted_context.v1"
	context.Objective = boundedInterruptedText(context.Objective)
	context.Status = boundedInterruptedText(context.Status)
	context.StopReason = boundedInterruptedText(context.StopReason)
	context.LastError = boundedInterruptedText(context.LastError)
	context.CompletedSteps = boundedInterruptedStrings(context.CompletedSteps)
	context.Evidence = boundedInterruptedStrings(context.Evidence)
	context.RelevantPaths = boundedInterruptedStrings(context.RelevantPaths)
	context.PendingWork = boundedInterruptedStrings(context.PendingWork)
	if len(context.Usage) > maxInterruptedUsageBytes || !json.Valid(context.Usage) {
		context.Usage = nil
	}
	if context.Objective == "" || context.Status == "" {
		return nil, errors.New("interrupted context objective or status is empty")
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return nil, fmt.Errorf("encode interrupted context: %w", err)
	}
	if len(encoded) > MaxInterruptedContextBytes {
		return nil, fmt.Errorf("interrupted context exceeds %d byte limit", MaxInterruptedContextBytes)
	}
	return encoded, nil
}

func boundedInterruptedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, min(len(values), maxInterruptedItems))
	for _, value := range values {
		value = boundedInterruptedText(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == maxInterruptedItems {
			break
		}
	}
	return result
}

func boundedInterruptedText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxInterruptedTextRunes {
		return string(runes[:maxInterruptedTextRunes]) + "…"
	}
	return value
}

func DecodeInterruptedContext(content json.RawMessage) (InterruptedContextV1, error) {
	if len(content) == 0 {
		return InterruptedContextV1{}, errors.New("interrupted context is empty")
	}
	if len(content) > MaxInterruptedContextBytes {
		return InterruptedContextV1{}, fmt.Errorf("interrupted context exceeds %d byte limit", MaxInterruptedContextBytes)
	}
	var decoded InterruptedContextV1
	if err := json.Unmarshal(content, &decoded); err != nil {
		return InterruptedContextV1{}, fmt.Errorf("decode interrupted context: %w", err)
	}
	if decoded.Type != "amadeus.interrupted_context.v1" || strings.TrimSpace(decoded.Objective) == "" || strings.TrimSpace(decoded.Status) == "" {
		return InterruptedContextV1{}, errors.New("interrupted context schema is invalid")
	}
	return decoded, nil
}
