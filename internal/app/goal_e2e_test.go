package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	goalextension "github.com/Godric-W/Amadeus/internal/extension/goal"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
)

type goalCompletingClient struct {
	mu       sync.Mutex
	requests []llm.Request
}

func (*goalCompletingClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *goalCompletingClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.mu.Lock()
	client.requests = append(client.requests, request)
	requestNumber := len(client.requests)
	client.mu.Unlock()
	usage := llm.TokenUsage{InputTokens: int64(10 * requestNumber), OutputTokens: 5, TotalTokens: int64(10*requestNumber + 5)}
	if requestNumber == 1 {
		return &appTestStream{chunks: []llm.StreamChunk{
			{ToolCalls: []llm.ToolCall{{ID: "call-complete", Name: "update_goal", Arguments: json.RawMessage(`{"status":"complete"}`)}}, FinishReason: llm.FinishReasonToolCalls, TokenUsage: &usage},
		}}, nil
	}
	return &appTestStream{chunks: []llm.StreamChunk{{ContentDelta: "goal delivered"}, {FinishReason: llm.FinishReasonStop, TokenUsage: &usage}}}, nil
}

func (*goalCompletingClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 128_000}
}

func (*goalCompletingClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

func TestGoalSetRunsPhysicalTurnAndStopsAfterUpdateGoal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &goalCompletingClient{}
	workspace, configuration := newInteractiveTestWorkspaceWithClient(t, ctx, client)
	application, err := NewInteractiveApplication(ctx, InteractiveOptions{Workspace: workspace, Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Start(ctx); err != nil {
		t.Fatal(err)
	}
	status := protocol.ThreadGoalActive
	created, err := application.SetGoal(ctx, goalextension.ObjectiveUpdate{Set: true, Value: "finish the integration"}, &status, state.TokenBudgetUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != protocol.ThreadGoalActive {
		t.Fatalf("created goal = %#v", created)
	}
	var observed []protocol.EventMsg
	for {
		event := waitInteractiveEvent[SessionEventObserved](t, application.Events(), nil)
		observed = append(observed, event.Event.Msg)
		if completed, ok := event.Event.Msg.(protocol.TurnCompleteEvent); ok && completed.LastAgentMessage != nil && *completed.LastAgentMessage == "goal delivered" {
			break
		}
	}
	goal, err := application.Goal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if goal == nil || goal.Status != protocol.ThreadGoalComplete || goal.TokensUsed <= 0 {
		t.Fatalf("final goal = %#v", goal)
	}
	goalUpdatedIndex, turnStartedIndex := -1, -1
	for index, message := range observed {
		switch message.(type) {
		case protocol.ThreadGoalUpdatedEvent:
			if goalUpdatedIndex < 0 {
				goalUpdatedIndex = index
			}
		case protocol.TurnStartedEvent:
			if turnStartedIndex < 0 {
				turnStartedIndex = index
			}
		}
	}
	if goalUpdatedIndex < 0 || turnStartedIndex < 0 || goalUpdatedIndex > turnStartedIndex {
		t.Fatalf("Goal/Turn event order = %#v", observed)
	}
	current, ok := workspace.Current()
	if !ok {
		t.Fatal("current thread missing")
	}
	history, err := current.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range history {
		if item, ok := line.Item.(rollout.EventMsgItem); ok {
			if completed, ok := item.Msg.(protocol.ItemCompletedEvent); ok && completed.Item.ToolName == "update_goal" {
				t.Fatalf("control Goal Tool leaked a visible completion item: %#v", completed.Item)
			}
		}
		if response, ok := line.Item.(rollout.ResponseItem); ok && response.Type == rollout.ResponseUserMessage {
			t.Fatalf("automatic Goal turn persisted a fake user message: %#v", response)
		}
	}
	client.mu.Lock()
	requests := append([]llm.Request(nil), client.requests...)
	client.mu.Unlock()
	if len(requests) != 2 || !promptContains(requests[0], "finish the integration") || !requestHasTool(requests[0], "update_goal") {
		t.Fatalf("requests = %#v", requests)
	}
	select {
	case event := <-application.Events():
		if observed, ok := event.(SessionEventObserved); ok {
			if _, started := observed.Event.Msg.(protocol.TurnStartedEvent); started {
				t.Fatal("complete goal started another turn")
			}
		}
	case <-time.After(100 * time.Millisecond):
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func promptContains(request llm.Request, value string) bool {
	for _, item := range request.Prompt.Input {
		if strings.Contains(item.Content, value) {
			return true
		}
	}
	return false
}

func requestHasTool(request llm.Request, name string) bool {
	for _, spec := range request.Prompt.Tools {
		if spec.Name == name {
			return true
		}
	}
	return false
}
