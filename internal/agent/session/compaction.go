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
	"github.com/Godric-W/Amadeus/internal/agent/modelclient"
	contextmanager "github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
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
	modelSession *modelclient.ModelClientSession,
	turnContext TurnContext,
	step *StepContext,
	prompt *contextmanager.PromptSnapshot,
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
	if prompt == nil {
		captured := session.promptSnapshot(*step)
		prompt = &captured
	}
	source, err := session.compactionSource(*prompt)
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
		Prompt: llm.Prompt{BaseInstructions: session.BaseInstructions()}, Model: step.Model,
		ModelSession: modelSession, Reasoning: llm.ReasoningConfigForEffort(turnContext.ReasoningEffort),
		Metadata: requestMetadata(turnContext), Events: events, Estimator: contextmanager.ApproxTokenEstimator{},
	})
	if output.TokenUsage.TotalTokens > 0 {
		activeTokens := output.TokenUsage.InputTokens
		if err := session.recordTokenUsage(context.WithoutCancel(ctx), turnContext.TurnID, output.TokenUsage, activeTokens, step.Model.ContextWindow, prompt.HistoryVersion, events); err != nil {
			return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, errors.Join(generateErr, err))
		}
	}
	if generateErr != nil {
		return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, generateErr)
	}
	if err := output.Validate(); err != nil {
		return false, session.finishCompactionFailure(events, itemID, startedAt, invocation, err)
	}
	before := session.contextWindowTokenStatus(*step, *prompt).ActiveContextTokens
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

func (session *Session) compactionSource(prompt contextmanager.PromptSnapshot) (agentcompact.Source, error) {
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
		if origin == contextmanager.MessageOriginUser {
			users = append(users, projection.Messages[index])
		}
	}
	return agentcompact.Source{
		HistoryVersion: prompt.HistoryVersion, CoveredThroughSequence: covered,
		SourceHash: hex.EncodeToString(digest[:]), CanonicalHistory: projection.Messages, UserMessages: users,
		PromptItems: prompt.Items,
	}, nil
}

func (session *Session) installCompaction(
	ctx context.Context,
	turnContext TurnContext,
	step StepContext,
	source agentcompact.Source,
	output agentcompact.Output,
	invocation compactionInvocation,
	before int64,
	events protocol.EventSink,
) error {
	replacement, origins, worldStateItem, err := session.compactionReplacement(step, invocation.Phase, output.ReplacementHistory)
	if err != nil {
		return err
	}
	compacted := rollout.CompactedItem{
		Trigger: invocation.Trigger, Reason: invocation.Reason, Phase: invocation.Phase,
		Summary: strings.TrimSpace(output.Message.Content), ReplacementHistory: replacement, ReplacementOrigins: origins,
		CoveredThroughSequence: source.CoveredThroughSequence, SourceHash: source.SourceHash,
		Provider: step.Model.Provider, Model: step.Model.Name, ResetWorldState: worldStateItem == nil, ResetTurnContext: worldStateItem == nil,
	}
	promptShape := session.promptShapeForStep(step)
	firstSequence := session.state.Context.NextSequence()
	installItems := []rollout.RolloutItem{rollout.ScopeItem(compacted, session.threadID, turnContext.TurnID)}
	if worldStateItem != nil {
		installItems = append(installItems, rollout.ScopeItem(*worldStateItem, session.threadID, turnContext.TurnID))
		installItems = append(installItems, rollout.ScopeItem(turnContextItem(turnContext), session.threadID, turnContext.TurnID))
	}
	preview, err := session.state.Context.PreviewRecord(firstSequence, step.Model, promptShape, installItems...)
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
		ObservedThroughSequence: firstSequence + uint64(len(installItems)) - 1,
	}
	installItems = append(installItems, rollout.EventMsgItem{Msg: tokenEvent})
	if err := session.appendItemsDurable(ctx, turnContext.TurnID, installItems...); err != nil {
		return &agentcompact.Error{Kind: agentcompact.ErrorPersistence, Err: err}
	}
	if err := events.Publish(context.WithoutCancel(ctx), protocol.Event{Msg: tokenEvent}); err != nil {
		return err
	}
	return nil
}

func (session *Session) compactionReplacement(step StepContext, phase protocol.CompactionPhase, replacement []llm.ResponseItem) ([]llm.ResponseItem, []rollout.ReplacementOrigin, *rollout.WorldStateItem, error) {
	replacement = cloneLLMItems(replacement)
	origins := make([]rollout.ReplacementOrigin, len(replacement))
	for index := range origins {
		origins[index] = rollout.ReplacementOriginUser
	}
	if len(origins) > 0 {
		origins[len(origins)-1] = rollout.ReplacementOriginCompaction
	}
	if phase != protocol.CompactionPhaseMidTurn {
		return replacement, origins, nil, nil
	}
	state, err := buildStepWorldState(&session.services, step)
	if err != nil {
		return nil, nil, nil, err
	}
	fragments, snapshot, err := state.Render(nil, contextmanager.PreviousSectionAbsent)
	if err != nil {
		return nil, nil, nil, err
	}
	contextItems, err := mergeContextFragments(fragments)
	if err != nil {
		return nil, nil, nil, err
	}
	insertion := len(replacement) - 1
	for index := len(origins) - 1; index >= 0; index-- {
		if origins[index] == rollout.ReplacementOriginUser {
			insertion = index
			break
		}
	}
	if insertion < 0 {
		insertion = 0
	}
	replacement = append(replacement, make([]llm.ResponseItem, len(contextItems))...)
	copy(replacement[insertion+len(contextItems):], replacement[insertion:len(replacement)-len(contextItems)])
	copy(replacement[insertion:], contextItems)
	contextOrigins := make([]rollout.ReplacementOrigin, len(contextItems))
	for index := range contextOrigins {
		contextOrigins[index] = rollout.ReplacementOriginRuntime
	}
	origins = append(origins, make([]rollout.ReplacementOrigin, len(contextOrigins))...)
	copy(origins[insertion+len(contextOrigins):], origins[insertion:len(origins)-len(contextOrigins)])
	copy(origins[insertion:], contextOrigins)
	return replacement, origins, &rollout.WorldStateItem{Full: true, Sections: snapshot.Clone()}, nil
}

func cloneLLMItems(items []llm.ResponseItem) []llm.ResponseItem {
	cloned := make([]llm.ResponseItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Parts = append([]llm.ContentPart(nil), item.Parts...)
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), item.ToolCalls...)
	}
	return cloned
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
