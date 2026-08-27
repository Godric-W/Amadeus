package session

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	agentcompact "github.com/Godric-W/Amadeus/internal/agent/compact"
	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type failCompactionStore struct{ threadstore.ThreadStore }

func (store failCompactionStore) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, error) {
	for _, item := range items {
		if _, ok := item.(rollout.CompactedItem); ok {
			return threadstore.AppendResult{}, errors.New("compaction append failed")
		}
	}
	return store.ThreadStore.AppendItems(ctx, id, turnID, items...)
}

func TestMidTurnCompactionUsesFrozenStepWorldState(t *testing.T) {
	messages := continuationModelMessages(t)
	session := newContinuationTestSession(t, &continuationTestClient{messages: messages}, nil, continuationModelInfo(messages), continuationProvider(0), DefaultTurnBudget())
	turnContext := continuationTurnContext(session, "turn-frozen", ModeKindDefault)
	step, err := session.services.CaptureStep(context.Background(), turnContext)
	if err != nil {
		t.Fatal(err)
	}
	step.PermissionProfile = project.PermissionProfile{ReadHost: true}
	key, ok := policy.NewCommandApprovalKey("go test ./...", turnContext.CWD)
	if !ok {
		t.Fatal("test command approval key is invalid")
	}
	session.services.permissions.ApplyCommandGrant(key)
	replacement, _, _, err := session.compactionReplacement(step, protocol.CompactionPhaseMidTurn, []llm.ResponseItem{
		llm.UserMessage("objective"), llm.UserMessage("summary"),
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, item := range replacement {
		joined += item.Content
	}
	if !strings.Contains(joined, `"session_command_approval_count":0`) || strings.Contains(joined, `"session_command_approval_count":1`) {
		t.Fatalf("mid-turn context did not use frozen StepContext: %s", joined)
	}
}

func TestCompactionPersistenceFailureKeepsOriginalHistoryAndPublishesFailedItem(t *testing.T) {
	messages := continuationModelMessages(t)
	usage := llm.TokenUsage{InputTokens: 6_000, OutputTokens: 20, TotalTokens: 6_020}
	client := &continuationTestClient{messages: messages, streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ContentDelta: "checkpoint"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop, TokenUsage: &usage}),
	}}
	model := continuationModelInfo(messages)
	session := newContinuationTestSessionWithStore(t, client, nil, model, continuationProvider(0), DefaultTurnBudget(), func(store threadstore.ThreadStore) threadstore.ThreadStore {
		return failCompactionStore{ThreadStore: store}
	})
	appendContinuationUser(t, session, "turn-1", "original objective")
	appendContinuationAssistant(t, session, "turn-1", strings.Repeat("details ", 1_000))
	turnContext := continuationTurnContext(session, "turn-2", ModeKindDefault)
	if err := session.AppendItems(context.Background(), turnContext.TurnID, turnContextItem(turnContext)); err != nil {
		t.Fatal(err)
	}
	step, err := session.captureStep(context.Background(), &session.services, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	before := session.ContextProjection()
	modelSession, err := session.services.NewModelClientSession()
	if err != nil {
		t.Fatal(err)
	}
	events := &continuationEventSink{session: session}
	prompt := session.promptSnapshot(step)
	_, err = session.runCompaction(context.Background(), &session.services, modelSession, turnContext, &step, &prompt, events, compactionInvocation{
		Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn,
	})
	var compactErr *agentcompact.Error
	if !errors.As(err, &compactErr) || compactErr.Kind != agentcompact.ErrorPersistence {
		t.Fatalf("compaction error = %#v", err)
	}
	if !reflect.DeepEqual(session.ContextProjection(), before) {
		t.Fatalf("failed install changed conversation: before=%#v after=%#v", before, session.ContextProjection())
	}
	failed := false
	for _, event := range events.events {
		completed, ok := event.Msg.(protocol.ItemCompletedEvent)
		if ok && completed.Item.Kind == protocol.ItemContextCompaction && completed.Item.Status == protocol.ItemFailed {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("failed compaction item was not published: %#v", events.events)
	}
}

func TestCompactionRejectsReplacementThatDoesNotReduceContext(t *testing.T) {
	messages := continuationModelMessages(t)
	client := &continuationTestClient{messages: messages, streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ContentDelta: strings.Repeat("a much longer checkpoint ", 2_000)}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(messages)
	session := newContinuationTestSession(t, client, nil, model, continuationProvider(0), DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-1", "short objective")
	appendContinuationAssistant(t, session, "turn-1", "done")
	turnContext := continuationTurnContext(session, "turn-2", ModeKindDefault)
	if err := session.AppendItems(context.Background(), turnContext.TurnID, turnContextItem(turnContext)); err != nil {
		t.Fatal(err)
	}
	step, err := session.captureStep(context.Background(), &session.services, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	modelSession, err := session.services.NewModelClientSession()
	if err != nil {
		t.Fatal(err)
	}
	prompt := session.promptSnapshot(step)
	_, err = session.runCompaction(context.Background(), &session.services, modelSession, turnContext, &step, &prompt, &continuationEventSink{session: session}, compactionInvocation{
		Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn,
	})
	var compactErr *agentcompact.Error
	if !errors.As(err, &compactErr) || compactErr.Kind != agentcompact.ErrorInsufficient {
		t.Fatalf("compaction error = %#v", err)
	}
}

func TestManualCompactionClearsWorldStateAndTurnContextBaselines(t *testing.T) {
	messages := continuationModelMessages(t)
	client := &continuationTestClient{messages: messages, streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ContentDelta: "manual checkpoint"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(messages)
	session := newContinuationTestSession(t, client, nil, model, continuationProvider(0), DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-1", "objective")
	appendContinuationAssistant(t, session, "turn-1", strings.Repeat("details ", 500))
	turnContext := continuationTurnContext(session, "turn-2", ModeKindDefault)
	if err := session.AppendItems(context.Background(), turnContext.TurnID, turnContextItem(turnContext)); err != nil {
		t.Fatal(err)
	}
	step, err := session.captureStep(context.Background(), &session.services, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := session.WorldStateBaseline(); !known || session.state.Context.ReferenceTurnContext() == nil {
		t.Fatal("pre-compaction baselines were not established")
	}
	modelSession, _ := session.services.NewModelClientSession()
	prompt := session.promptSnapshot(step)
	if _, err := session.runCompaction(context.Background(), &session.services, modelSession, turnContext, &step, &prompt, &continuationEventSink{session: session}, compactionInvocation{
		Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn,
	}); err != nil {
		t.Fatal(err)
	}
	if _, known := session.WorldStateBaseline(); known || session.state.Context.ReferenceTurnContext() != nil {
		t.Fatal("manual compaction retained context baselines")
	}
}

func TestMidTurnCompactionInstallsFullContextBeforeLastUserAndKeepsSummaryLast(t *testing.T) {
	messages := continuationModelMessages(t)
	client := &continuationTestClient{messages: messages, streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ContentDelta: "mid-turn checkpoint"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(messages)
	session := newContinuationTestSession(t, client, nil, model, continuationProvider(0), DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-1", "latest objective")
	appendContinuationAssistant(t, session, "turn-1", strings.Repeat("details ", 500))
	turnContext := continuationTurnContext(session, "turn-2", ModeKindDefault)
	if err := session.AppendItems(context.Background(), turnContext.TurnID, turnContextItem(turnContext)); err != nil {
		t.Fatal(err)
	}
	step, err := session.captureStep(context.Background(), &session.services, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	modelSession, _ := session.services.NewModelClientSession()
	prompt := session.promptSnapshot(step)
	if _, err := session.runCompaction(context.Background(), &session.services, modelSession, turnContext, &step, &prompt, &continuationEventSink{session: session}, compactionInvocation{
		Trigger: protocol.CompactionTriggerAuto, Reason: protocol.CompactionReasonContextLimit, Phase: protocol.CompactionPhaseMidTurn,
	}); err != nil {
		t.Fatal(err)
	}
	if _, known := session.WorldStateBaseline(); !known || session.state.Context.ReferenceTurnContext() == nil {
		t.Fatal("mid-turn compaction did not install context baselines")
	}
	projection := session.ContextProjection()
	if len(projection.Messages) < 3 || projection.Messages[len(projection.Messages)-1].Role != llm.RoleUser || !strings.Contains(projection.Messages[len(projection.Messages)-1].Content, "Another language model started") {
		t.Fatalf("mid-turn replacement does not end with summary: %#v", projection.Messages)
	}
	userIndex, contextBeforeUser := -1, false
	for index, item := range projection.Messages[:len(projection.Messages)-1] {
		if item.Content == "latest objective" {
			userIndex = index
			break
		}
		if strings.Contains(item.Content, "<collaboration_mode>") || strings.Contains(item.Content, "<environment_context>") {
			contextBeforeUser = true
		}
	}
	if userIndex < 0 || !contextBeforeUser || projection.Origins[len(projection.Origins)-1] != contextmanager.MessageOriginCompaction {
		t.Fatalf("mid-turn context placement/origins are invalid: messages=%#v origins=%#v", projection.Messages, projection.Origins)
	}
}
