package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcompact "github.com/Godric-W/Amadeus/internal/agent/compact"
	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type compactionInvocation struct {
	Trigger protocol.CompactionTrigger
	Reason  protocol.CompactionReason
	Phase   protocol.CompactionPhase
}

func (session *Session) runCompaction(
	ctx context.Context,
	runtime *SessionServices,
	modelSession *engine.ModelClientSession,
	turnContext turn.TurnContext,
	step *engine.StepContext,
	events protocol.EventSink,
	invocation compactionInvocation,
) (bool, error) {
	if session == nil || runtime == nil || runtime.compaction == nil || modelSession == nil || events == nil {
		return false, errors.New("compaction runtime is incomplete")
	}
	if !invocation.Trigger.Valid() || !invocation.Reason.Valid() || !invocation.Phase.Valid() {
		return false, errors.New("compaction lifecycle is invalid")
	}
	if step == nil {
		captured, err := session.captureStep(ctx, runtime, turnContext)
		if err != nil {
			return false, err
		}
		step = &captured
	}
	source, err := session.compactionSource(*step)
	if err != nil {
		return false, err
	}
	itemID := protocol.ItemID(session.services.NextID("item"))
	startedAt := session.services.Clock().UTC()
	started := compactionTurnItem(itemID, protocol.ItemInProgress, startedAt, time.Time{}, invocation, "")
	if err := events.Publish(ctx, protocol.Event{Msg: protocol.ItemStartedEvent{Item: started}}); err != nil {
		return false, err
	}
	output, generateErr := runtime.compaction.Generate(ctx, agentcompact.Request{
		Trigger: invocation.Trigger, Reason: invocation.Reason, Phase: invocation.Phase,
		Source: source,
		Prompt: llm.Prompt{BaseInstructions: step.BaseInstructions}, Model: step.Model,
		ModelSession: modelSession, Reasoning: llm.ReasoningConfigForEffort(turnContext.ReasoningEffort),
		Metadata: requestMetadata(turnContext), Events: events, Estimator: agentcontext.ApproxTokenEstimator{},
	})
	if output.TokenUsage.TotalTokens > 0 {
		activeTokens := output.TokenUsage.InputTokens
		if err := session.recordTokenUsage(context.WithoutCancel(ctx), turnContext.TurnID, output.TokenUsage, activeTokens, step.Model.ContextWindow, step.Prompt.HistoryVersion, events); err != nil {
			return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, errors.Join(generateErr, err))
		}
	}
	if generateErr != nil {
		return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, generateErr)
	}
	if err := output.Validate(); err != nil {
		return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, err)
	}
	before := session.contextWindowTokenStatus(*step).ActiveContextTokens
	if err := session.installCompaction(ctx, turnContext, *step, source, output, invocation, before, events); err != nil {
		return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, err)
	}
	completedAt := session.services.Clock().UTC()
	completed := compactionTurnItem(itemID, protocol.ItemStatusCompleted, startedAt, completedAt, invocation, "")
	if err := events.Publish(context.WithoutCancel(ctx), protocol.Event{Msg: protocol.ItemCompletedEvent{Item: completed}}); err != nil {
		return false, err
	}
	if err := events.Publish(context.WithoutCancel(ctx), protocol.Event{Msg: protocol.WarningEvent{Message: compactionWarningMessage}}); err != nil {
		return false, err
	}
	return true, nil
}

func (session *Session) compactionSource(step engine.StepContext) (agentcompact.Source, error) {
	projection := session.ContextProjection()
	if len(projection.Messages) == 0 || len(projection.SourceSequences) != len(projection.Messages) {
		return agentcompact.Source{}, &agentcompact.Error{Kind: agentcompact.ErrorNoHistory, Err: errors.New("conversation has no model-visible history")}
	}
	encoded, err := json.Marshal(projection.Messages)
	if err != nil {
		return agentcompact.Source{}, fmt.Errorf("encode compaction source: %w", err)
	}
	digest := sha256.Sum256(encoded)
	covered := projection.SourceSequences[len(projection.SourceSequences)-1]
	users := make([]llm.ResponseItem, 0)
	for index, origin := range projection.Origins {
		if origin == agentcontext.MessageOriginUser {
			users = append(users, projection.Messages[index])
		}
	}
	return agentcompact.Source{
		HistoryVersion: step.Prompt.HistoryVersion, CoveredThroughSequence: covered,
		SourceHash: hex.EncodeToString(digest[:]), CanonicalHistory: projection.Messages, UserMessages: users,
		PromptItems: step.Prompt.Items,
	}, nil
}

func (session *Session) installCompaction(
	ctx context.Context,
	turnContext turn.TurnContext,
	step engine.StepContext,
	source agentcompact.Source,
	output agentcompact.Output,
	invocation compactionInvocation,
	before int64,
	events protocol.EventSink,
) error {
	compacted := rollout.CompactedItem{
		Trigger: invocation.Trigger, Reason: invocation.Reason, Phase: invocation.Phase,
		Summary: strings.TrimSpace(output.Message.Content), ReplacementHistory: output.ReplacementHistory,
		CoveredThroughSequence: source.CoveredThroughSequence, SourceHash: source.SourceHash,
		Provider: step.Model.Provider, Model: step.Model.Name,
	}
	scopedCompacted := rollout.ScopeItem(compacted, session.threadID, turnContext.TurnID)
	promptShape := promptShapeForStep(step, turnContext)
	firstSequence := session.state.Context.NextSequence()
	preview, err := session.state.Context.PreviewRecord(firstSequence, step.Model, promptShape, scopedCompacted)
	if err != nil {
		return &agentcompact.Error{Kind: agentcompact.ErrorStale, Err: err}
	}
	after := preview.EstimatedInputTokens
	model := step.Model.Normalized()
	if before > 0 && after >= before {
		return &agentcompact.Error{Kind: agentcompact.ErrorInsufficient, Err: fmt.Errorf("active context did not decrease: before=%d after=%d", before, after)}
	}
	if model.ContextWindow > 0 && after >= model.ContextWindow {
		return &agentcompact.Error{Kind: agentcompact.ErrorInsufficient, Err: fmt.Errorf("replacement context %d exceeds window %d", after, model.ContextWindow)}
	}
	if invocation.Trigger == protocol.CompactionTriggerAuto && model.AutoCompactTokenLimit > 0 && after >= model.AutoCompactTokenLimit {
		return &agentcompact.Error{Kind: agentcompact.ErrorInsufficient, Err: fmt.Errorf("replacement context %d remains above compact limit %d", after, model.AutoCompactTokenLimit)}
	}
	tokenSnapshot := session.state.Context.TokenSnapshot()
	tokenEvent := protocol.TokenCountEvent{
		Info: cloneProtocolTokenUsageInfo(tokenSnapshot.Info), ActiveContextTokens: after, ActiveContextEstimated: true,
		ObservedThroughSequence: firstSequence,
	}
	if err := session.appendItemsDurable(ctx, turnContext.TurnID, compacted, rollout.EventMsgItem{Msg: tokenEvent}); err != nil {
		return &agentcompact.Error{Kind: agentcompact.ErrorPersistence, Err: err}
	}
	if err := events.Publish(context.WithoutCancel(ctx), protocol.Event{Msg: tokenEvent}); err != nil {
		return err
	}
	return nil
}

func promptShapeForStep(step engine.StepContext, turnContext turn.TurnContext) llm.Prompt {
	specs := step.ToolRouter.Specs()
	tools := make([]llm.ToolSpec, len(specs))
	for index, spec := range specs {
		tools[index] = llm.ToolSpec{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
	}
	return llm.Prompt{
		BaseInstructions: step.BaseInstructions, Tools: tools,
		ParallelToolCalls: step.Model.SupportsParallelToolCalls,
		OutputSchema:      append(llm.OutputSchema(nil), turnContext.OutputSchema...), OutputSchemaStrict: turnContext.OutputSchemaStrict,
	}
}

func (session *Session) finishCompactionFailure(events protocol.EventSink, itemID protocol.ItemID, startedAt time.Time, invocation compactionInvocation, cause error) error {
	completed := compactionTurnItem(itemID, protocol.ItemFailed, startedAt, session.services.Clock().UTC(), invocation, cause.Error())
	publishErr := events.Publish(context.WithoutCancel(session.ctx), protocol.Event{Msg: protocol.ItemCompletedEvent{Item: completed}})
	return errors.Join(cause, publishErr)
}

func compactionTurnItem(id protocol.ItemID, status protocol.ItemStatus, startedAt, completedAt time.Time, invocation compactionInvocation, text string) protocol.TurnItem {
	return protocol.TurnItem{
		ID: id, Kind: protocol.ItemContextCompaction, Status: status,
		CreatedAt: startedAt, CompletedAt: completedAt, Text: text,
		Payload: protocol.ContextCompactionItem{Trigger: invocation.Trigger, Reason: invocation.Reason, Phase: invocation.Phase},
	}
}
