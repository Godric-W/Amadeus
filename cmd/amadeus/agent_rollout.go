package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

type sessionRolloutRecorder struct {
	runtime *sessiondomain.SessionRuntime
	runID   sessiondomain.RunID
	nextID  func(string) string
	clock   func() time.Time
}

func (recorder *sessionRolloutRecorder) RecordToolCalls(ctx context.Context, message llm.Message) error {
	if recorder == nil || recorder.runtime == nil || recorder.runID == "" || recorder.nextID == nil || recorder.clock == nil {
		return errors.New("session rollout recorder is not configured")
	}
	if message.Role != llm.RoleAssistant || len(message.ToolCalls) == 0 {
		return errors.New("tool call rollout requires an assistant message with calls")
	}
	calls := make([]sessiondomain.ToolCallRecord, len(message.ToolCalls))
	for index, call := range message.ToolCalls {
		calls[index] = sessiondomain.ToolCallRecord{ID: call.ID, Name: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...)}
	}
	payload, err := sessiondomain.EncodePayload(sessiondomain.ToolCallPayload{Content: strings.TrimSpace(message.Content), Calls: calls})
	if err != nil {
		return err
	}
	_, err = recorder.runtime.Append(context.WithoutCancel(ctx), recorder.runID, sessiondomain.AppendItem{
		ID: sessiondomain.RolloutItemID(recorder.nextID("item")), RunID: recorder.runID,
		Kind: sessiondomain.RolloutToolCall, Payload: payload, CreatedAt: recorder.clock().UTC(),
	})
	return err
}

func (recorder *sessionRolloutRecorder) RecordToolOutcomes(ctx context.Context, outcomes []react.ToolOutcome) error {
	if recorder == nil || recorder.runtime == nil || recorder.runID == "" || recorder.nextID == nil || recorder.clock == nil {
		return errors.New("session rollout recorder is not configured")
	}
	drafts := make([]sessiondomain.AppendItem, 0, len(outcomes))
	for _, outcome := range outcomes {
		parts := make([]sessiondomain.ToolContentPart, len(outcome.Result.Parts))
		for index, part := range outcome.Result.Parts {
			parts[index] = sessiondomain.ToolContentPart{Kind: string(part.Kind), Text: part.Text, MediaType: part.MediaType, Data: part.Data}
		}
		var toolError *sessiondomain.ToolErrorPayload
		if outcome.Error != nil {
			toolError = &sessiondomain.ToolErrorPayload{Kind: outcome.Error.Kind, Message: outcome.Error.Message}
		}
		payload, err := sessiondomain.EncodePayload(sessiondomain.ToolResultPayload{
			CallID: outcome.CallID, ToolName: outcome.ToolName, Status: string(outcome.Status),
			Text: outcome.Result.Text, Parts: parts, Metadata: outcome.Metadata,
			Error: toolError, Partial: outcome.Partial, Duration: int64(outcome.Duration),
		})
		if err != nil {
			return err
		}
		drafts = append(drafts, sessiondomain.AppendItem{
			ID: sessiondomain.RolloutItemID(recorder.nextID("item")), RunID: recorder.runID,
			Kind: sessiondomain.RolloutToolResult, Payload: payload, CreatedAt: recorder.clock().UTC(),
		})
	}
	if len(drafts) == 0 {
		return nil
	}
	_, err := recorder.runtime.Append(context.WithoutCancel(ctx), recorder.runID, drafts...)
	return err
}

func (runner *agentController) persistCompaction(ctx context.Context, sessionRuntime *sessiondomain.SessionRuntime, started sessiondomain.StartedRun, projection sessiondomain.MessageProjection, envelope agentcontext.Envelope, provider, model string) error {
	if envelope.Compaction == nil || envelope.Compaction.CoveredMessages < 1 || envelope.Compaction.CoveredMessages > len(projection.SourceSequences) {
		return nil
	}
	coveredTo := envelope.Compaction.CoveredThroughSequence
	if coveredTo == 0 {
		coveredTo = projection.SourceSequences[envelope.Compaction.CoveredMessages-1]
	}
	replacements := make([]sessiondomain.CompactionHistoryItem, 0, len(envelope.Compaction.ReplacementHistory))
	for _, message := range envelope.Compaction.ReplacementHistory {
		replacements = append(replacements, sessiondomain.CompactionHistoryItem{Role: message.Role, Content: message.Content})
	}
	if len(replacements) == 0 {
		replacements = append(replacements, sessiondomain.CompactionHistoryItem{Role: llm.RoleAssistant, Content: envelope.Compaction.Summary})
	}
	payload, err := sessiondomain.EncodePayload(sessiondomain.ContextCompactionPayload{
		Summary:                envelope.Compaction.Summary,
		ReplacementHistory:     replacements,
		CoveredThroughSequence: coveredTo, SourceHash: envelope.Compaction.SourceHash, Provider: provider, Model: model,
	})
	if err != nil {
		return err
	}
	_, err = sessionRuntime.Append(context.WithoutCancel(ctx), started.Records.Run.ID, sessiondomain.AppendItem{
		ID: sessiondomain.RolloutItemID(runner.runtimeID("item")), RunID: started.Records.Run.ID,
		Kind: sessiondomain.RolloutContextCompaction, Payload: payload, CreatedAt: runner.runtimeNow(),
	})
	return err
}
