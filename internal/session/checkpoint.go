package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CheckpointSchemaVersion   = 1
	MaxCheckpointPayloadBytes = 256 << 10
)

type CheckpointStep struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type CheckpointEvidence struct {
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary"`
	Paths    []string `json:"paths,omitempty"`
	Verified bool     `json:"verified"`
}

type CheckpointToolSummary struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type CheckpointPayloadV1 struct {
	Type             string                  `json:"type"`
	Objective        string                  `json:"objective"`
	Status           string                  `json:"status"`
	StopReason       string                  `json:"stop_reason,omitempty"`
	CompletedSteps   []CheckpointStep        `json:"completed_steps,omitempty"`
	Evidence         []CheckpointEvidence    `json:"evidence,omitempty"`
	ToolSummaries    []CheckpointToolSummary `json:"tool_summaries,omitempty"`
	RelevantPaths    []string                `json:"relevant_paths,omitempty"`
	PendingWork      []string                `json:"pending_work,omitempty"`
	Usage            json.RawMessage         `json:"usage,omitempty"`
	ConversationRefs []MessageID             `json:"conversation_refs,omitempty"`
}

func NewCheckpoint(id CheckpointID, runID RunID, sequence int64, reason CheckpointReason, payload CheckpointPayloadV1, createdAt time.Time) (Checkpoint, error) {
	payload.Type = "amadeus.run_checkpoint.v1"
	payload.Objective = strings.TrimSpace(payload.Objective)
	payload.Status = strings.TrimSpace(payload.Status)
	payload.StopReason = strings.TrimSpace(payload.StopReason)
	if payload.Objective == "" || payload.Status == "" {
		return Checkpoint{}, errors.New("checkpoint payload objective or status is empty")
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("marshal checkpoint payload: %w", err)
	}
	if len(content) > MaxCheckpointPayloadBytes {
		return Checkpoint{}, fmt.Errorf("checkpoint payload exceeds %d byte limit", MaxCheckpointPayloadBytes)
	}
	digest := sha256.Sum256(content)
	checkpoint := Checkpoint{
		ID: id, RunID: runID, Sequence: sequence, SchemaVersion: CheckpointSchemaVersion,
		Reason: reason, PayloadJSON: content, PayloadHash: hex.EncodeToString(digest[:]), CreatedAt: createdAt.UTC(),
	}
	return checkpoint, checkpoint.Validate()
}

func (checkpoint Checkpoint) VerifyPayload() error {
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	if len(checkpoint.PayloadJSON) > MaxCheckpointPayloadBytes {
		return fmt.Errorf("checkpoint payload exceeds %d byte limit", MaxCheckpointPayloadBytes)
	}
	digest := sha256.Sum256(checkpoint.PayloadJSON)
	if hex.EncodeToString(digest[:]) != checkpoint.PayloadHash {
		return errors.New("checkpoint payload SHA-256 does not match payload")
	}
	return nil
}
