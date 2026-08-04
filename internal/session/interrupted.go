package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const MaxPreviousWorkBytes = 256 << 10

const (
	maxPreviousWorkItems      = 64
	maxPreviousWorkTextRunes  = 1024
	maxPreviousWorkUsageBytes = 16 << 10
)

type PreviousWork struct {
	Type                 string          `json:"type"`
	Objective            string          `json:"objective"`
	Status               string          `json:"status"`
	StopReason           string          `json:"stop_reason,omitempty"`
	CompletedWork        []string        `json:"completed_work,omitempty"`
	Evidence             []string        `json:"evidence,omitempty"`
	RelevantPaths        []string        `json:"relevant_paths,omitempty"`
	PendingWork          []string        `json:"pending_work,omitempty"`
	Usage                json.RawMessage `json:"usage,omitempty"`
	LastError            string          `json:"last_error,omitempty"`
	LegacyCompletedSteps []string        `json:"completed_steps,omitempty"`
}

func EncodePreviousWork(work PreviousWork) (json.RawMessage, error) {
	work.Type = "amadeus.previous_work.v1"
	work.Objective = boundedPreviousWorkText(work.Objective)
	work.Status = boundedPreviousWorkText(work.Status)
	work.StopReason = boundedPreviousWorkText(work.StopReason)
	work.LastError = boundedPreviousWorkText(work.LastError)
	work.CompletedWork = boundedPreviousWorkStrings(append(work.CompletedWork, work.LegacyCompletedSteps...))
	work.LegacyCompletedSteps = nil
	work.Evidence = boundedPreviousWorkStrings(work.Evidence)
	work.RelevantPaths = boundedPreviousWorkStrings(work.RelevantPaths)
	work.PendingWork = boundedPreviousWorkStrings(work.PendingWork)
	if len(work.Usage) > maxPreviousWorkUsageBytes || !json.Valid(work.Usage) {
		work.Usage = nil
	}
	if work.Objective == "" || work.Status == "" {
		return nil, errors.New("previous work objective or status is empty")
	}
	encoded, err := json.Marshal(work)
	if err != nil {
		return nil, fmt.Errorf("encode previous work: %w", err)
	}
	if len(encoded) > MaxPreviousWorkBytes {
		return nil, fmt.Errorf("previous work exceeds %d byte limit", MaxPreviousWorkBytes)
	}
	return encoded, nil
}

func DecodePreviousWork(content json.RawMessage) (PreviousWork, error) {
	if len(content) == 0 {
		return PreviousWork{}, errors.New("previous work is empty")
	}
	if len(content) > MaxPreviousWorkBytes {
		return PreviousWork{}, fmt.Errorf("previous work exceeds %d byte limit", MaxPreviousWorkBytes)
	}
	var decoded PreviousWork
	if err := json.Unmarshal(content, &decoded); err != nil {
		return PreviousWork{}, fmt.Errorf("decode previous work: %w", err)
	}
	if decoded.Type != "amadeus.previous_work.v1" && decoded.Type != "amadeus.interrupted_context.v1" {
		return PreviousWork{}, errors.New("previous work schema is invalid")
	}
	if strings.TrimSpace(decoded.Objective) == "" || strings.TrimSpace(decoded.Status) == "" {
		return PreviousWork{}, errors.New("previous work schema is invalid")
	}
	decoded.CompletedWork = boundedPreviousWorkStrings(append(decoded.CompletedWork, decoded.LegacyCompletedSteps...))
	decoded.LegacyCompletedSteps = nil
	return decoded, nil
}

func boundedPreviousWorkStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, min(len(values), maxPreviousWorkItems))
	for _, value := range values {
		value = boundedPreviousWorkText(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == maxPreviousWorkItems {
			break
		}
	}
	return result
}

func boundedPreviousWorkText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxPreviousWorkTextRunes {
		return string(runes[:maxPreviousWorkTextRunes]) + "…"
	}
	return value
}
