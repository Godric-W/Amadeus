package session

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	agentcompact "github.com/Godric-W/Amadeus/internal/agent/compact"
	"github.com/Godric-W/Amadeus/internal/llm"
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
	before := session.ContextProjection()
	turnContext := continuationTurnContext(session, "turn-2", ModeKindDefault)
	step, err := session.captureStep(context.Background(), &session.services, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	modelSession, err := session.services.NewModelClientSession()
	if err != nil {
		t.Fatal(err)
	}
	events := &continuationEventSink{session: session}
	_, err = session.runCompaction(context.Background(), &session.services, modelSession, turnContext, &step, events, compactionInvocation{
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
		continuationStream(llm.StreamChunk{ContentDelta: "a longer checkpoint than the original response"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(messages)
	session := newContinuationTestSession(t, client, nil, model, continuationProvider(0), DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-1", "short objective")
	appendContinuationAssistant(t, session, "turn-1", "done")
	turnContext := continuationTurnContext(session, "turn-2", ModeKindDefault)
	step, err := session.captureStep(context.Background(), &session.services, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	modelSession, err := session.services.NewModelClientSession()
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.runCompaction(context.Background(), &session.services, modelSession, turnContext, &step, &continuationEventSink{session: session}, compactionInvocation{
		Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn,
	})
	var compactErr *agentcompact.Error
	if !errors.As(err, &compactErr) || compactErr.Kind != agentcompact.ErrorInsufficient {
		t.Fatalf("compaction error = %#v", err)
	}
}
