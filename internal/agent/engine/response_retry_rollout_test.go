package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestResponseRetryPersistsOnlySuccessfulAttempt(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{
		&scriptedModelStream{results: []scriptedStreamResult{
			{chunk: llm.StreamChunk{ContentDelta: "partial"}},
			{err: &llm.ProviderError{Kind: llm.ProviderErrorNetwork, Message: "reset", Retryable: true, RetryDelay: time.Nanosecond}},
		}},
		&scriptedModelStream{results: []scriptedStreamResult{
			{chunk: llm.StreamChunk{ContentDelta: "recovered"}},
			{chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}},
		}},
	}}
	registry := tool.NewRegistry()
	toolService, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Services{
		providerName: "test-provider",
		provider: config.ModelProviderInfo{
			StreamMaxRetries: 1, StreamIdleTimeout: time.Second,
		},
		modelInfo: llm.ModelInfo{Provider: "test-provider", Name: "test-model", ContextWindow: 100_000, AutoCompactTokenLimit: 90_000, ToolOutputTokenLimit: 10_000},
		client:    client, modelMessages: testModelMessages(t), registry: registry, toolService: toolService,
		visibility: map[string]bool{}, budget: DefaultTurnBudget(),
	}
	host := &engineTestHost{context: agentcontext.NewManager(nil)}
	user, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-1", user); err != nil {
		t.Fatal(err)
	}
	result, err := RunTurn(context.Background(), runtime, RunRequest{
		Snapshot: host.Snapshot, AppendItems: host.AppendItems, Events: host, Instructions: &engineTestScope{},
		Turn: turn.TurnContext{ThreadID: "thread-1", TurnID: "turn-1", Provider: "test-provider", Model: "test-model", CWD: t.TempDir(), Mode: turn.ModeKindDefault},
	})
	if err != nil || result.Outcome != rollout.TurnOutcomeCompleted {
		t.Fatalf("run result=%#v err=%v", result, err)
	}
	projection, err := agentcontext.ProjectRolloutMessages(host.History())
	if err != nil {
		t.Fatal(err)
	}
	var assistant []string
	for _, message := range projection.Messages {
		if message.Role == llm.RoleAssistant {
			assistant = append(assistant, message.Content)
		}
	}
	if len(assistant) != 1 || assistant[0] != "recovered" {
		t.Fatalf("canonical assistant history = %#v", assistant)
	}
}
