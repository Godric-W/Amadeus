package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type engineTestClient struct {
	mu       sync.Mutex
	requests []llm.Request
	streams  [][]llm.StreamChunk
}

func (*engineTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected complete call")
}

func (client *engineTestClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.requests = append(client.requests, request)
	if len(client.streams) == 0 {
		return nil, errors.New("no scripted stream")
	}
	chunks := client.streams[0]
	client.streams = client.streams[1:]
	return &engineTestStream{chunks: chunks}, nil
}

func (*engineTestClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "test", Name: "test-model", ContextWindow: 100_000, MaxOutputTokens: 1024, SupportsParallelToolCalls: true}
}

func (*engineTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsParallelToolCalls: true}
}

type engineTestStream struct {
	chunks []llm.StreamChunk
	index  int
}

func (stream *engineTestStream) Recv() (llm.StreamChunk, error) {
	if stream.index >= len(stream.chunks) {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[stream.index]
	stream.index++
	return chunk, nil
}

func (*engineTestStream) Close() error { return nil }

type engineTestTool struct{}

func (*engineTestTool) Spec() tool.ToolSpec {
	return tool.ToolSpec{Name: "unstable", Description: "always fails", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}
func (*engineTestTool) SupportsParallelToolCalls() bool                          { return true }
func (*engineTestTool) ValidateInput(tool.ToolUseContext, tool.Invocation) error { return nil }
func (*engineTestTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return tool.PreparedToolUse{Invocation: invocation, Permission: tool.AllowPermission()}, nil
}
func (*engineTestTool) Execute(tool.ToolUseContext, tool.PreparedToolUse) (tool.ToolResult, error) {
	return tool.ToolResult{}, errors.New("temporary failure")
}

type engineNamedTool struct {
	name   string
	effect tool.SideEffect
}

func (value *engineNamedTool) Spec() tool.ToolSpec {
	return tool.ToolSpec{Name: value.name, Description: "named test tool", InputSchema: json.RawMessage(`{"type":"object"}`), SideEffect: value.effect, Idempotent: true}
}
func (*engineNamedTool) SupportsParallelToolCalls() bool                          { return true }
func (*engineNamedTool) ValidateInput(tool.ToolUseContext, tool.Invocation) error { return nil }
func (*engineNamedTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return tool.PreparedToolUse{Invocation: invocation, Permission: tool.AllowPermission()}, nil
}
func (*engineNamedTool) Execute(_ tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	return tool.ToolResult{CallID: prepared.Invocation.Call.ID, ToolName: prepared.Invocation.Call.Name}, nil
}

type engineTestScope struct{ sampled int }

func (*engineTestScope) Ensure(context.Context, tool.ContextTarget) error { return nil }
func (scope *engineTestScope) MarkSampled()                               { scope.sampled++ }

type engineTestHost struct {
	mu      sync.Mutex
	context *agentcontext.Manager
	lines   []rollout.Line
	order   []string
}

func (host *engineTestHost) AppendItems(_ context.Context, turnID turn.ID, items ...rollout.Item) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, item := range items {
		host.lines = append(host.lines, rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(len(host.lines) + 1), Timestamp: time.Now().UTC(), ThreadID: "thread-1", TurnID: turnID, Item: item})
		host.order = append(host.order, "append:"+string(item.Kind))
	}
	return host.context.Rebuild(host.lines)
}

func (host *engineTestHost) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	return host.context.Snapshot(model, prompt)
}

func (host *engineTestHost) Publish(_ context.Context, event protocol.SessionEvent) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	switch message := event.Message.(type) {
	case protocol.ItemCompleted:
		host.order = append(host.order, "complete:"+string(message.Item.Kind))
	case protocol.ItemStarted:
		host.order = append(host.order, "start:"+string(message.Item.Kind))
	}
	return nil
}

func TestTurnEngineContinuesAfterToolFailureAndPersistsBeforeCompletion(t *testing.T) {
	client := &engineTestClient{streams: [][]llm.StreamChunk{
		{{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "unstable", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls, Usage: &llm.Usage{TotalTokens: 10}}},
		{{ContentDelta: "recovered"}, {FinishReason: llm.FinishReasonStop, Usage: &llm.Usage{TotalTokens: 5}}},
	}}
	registry := tool.NewRegistry()
	if err := registry.RegisterDefinition(&engineTestTool{}); err != nil {
		t.Fatal(err)
	}
	service, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{MaxParallel: 2})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Services{
		providerName: "test", provider: config.ProviderConfig{Model: "test-model", MaxOutputTokens: 1024}, client: client,
		baseInstructions: llm.BaseInstructions{Text: "test instructions"}, registry: registry,
		toolService: service, visibility: map[string]bool{},
	}
	host := &engineTestHost{context: agentcontext.NewManager(nil)}
	scope := &engineTestScope{}
	user, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-1", user); err != nil {
		t.Fatal(err)
	}
	result, err := RunTurn(context.Background(), runtime, RunRequest{
		Snapshot: host.Snapshot, AppendItems: host.AppendItems, Events: host, Instructions: scope,
		Turn: turn.TurnContext{ThreadID: "thread-1", TurnID: "turn-1", Provider: "test", Model: "test-model", CWD: t.TempDir(), Mode: turn.ModeKindDefault},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "result: completed" || result.Usage.TotalTokens != 15 || scope.sampled != 2 || len(client.requests) != 2 {
		t.Fatalf("turn result=%#v sampled=%d requests=%d", result, scope.sampled, len(client.requests))
	}
	secondInput := client.requests[1].Prompt.Input
	if len(secondInput) == 0 || secondInput[len(secondInput)-1].Role != llm.RoleTool || !strings.Contains(secondInput[len(secondInput)-1].Content, "temporary failure") {
		t.Fatalf("tool failure was not returned to model: %#v", secondInput)
	}
	host.mu.Lock()
	order := append([]string(nil), host.order...)
	host.mu.Unlock()
	assertBefore(t, order, "append:"+string(rollout.KindResponseItem), "complete:"+string(protocol.ItemToolCall))
	lastAppend := lastIndex(order, "append:"+string(rollout.KindResponseItem))
	lastComplete := lastIndex(order, "complete:"+string(protocol.ItemAssistantMessage))
	if lastAppend < 0 || lastComplete < 0 || lastAppend > lastComplete {
		t.Fatalf("assistant completion preceded canonical append: %v", order)
	}
}

func TestStepContextDerivesPlanMaskAndRevisionFromOneRegistrySnapshot(t *testing.T) {
	client := &engineTestClient{}
	registry := tool.NewRegistry()
	for _, definition := range []tool.ToolDefinition{
		&engineNamedTool{name: "read", effect: tool.SideEffectRead},
		&engineNamedTool{name: "write", effect: tool.SideEffectWrite},
		&engineNamedTool{name: "update_plan", effect: tool.SideEffectNone},
		&engineNamedTool{name: "web_search", effect: tool.SideEffectNetwork},
	} {
		if err := registry.RegisterDefinition(definition); err != nil {
			t.Fatal(err)
		}
	}
	runtime := &Services{client: client, baseInstructions: llm.BaseInstructions{Text: "base"}, registry: registry, visibility: map[string]bool{}, provider: config.ProviderConfig{MaxOutputTokens: 1024}}
	host := &engineTestHost{context: agentcontext.NewManager(nil)}
	regular, err := runtime.CaptureStep(host.Snapshot, turn.TurnContext{Mode: turn.ModeKindDefault})
	if err != nil {
		t.Fatal(err)
	}
	if !containsName(regular.ToolNames, "update_plan") || !containsName(regular.ToolNames, "write") {
		t.Fatalf("regular tool mask = %v", regular.ToolNames)
	}
	plan, err := runtime.CaptureStep(host.Snapshot, turn.TurnContext{Mode: turn.ModeKindPlan})
	if err != nil {
		t.Fatal(err)
	}
	if containsName(plan.ToolNames, "update_plan") || containsName(plan.ToolNames, "write") || !containsName(plan.ToolNames, "read") || !containsName(plan.ToolNames, "web_search") {
		t.Fatalf("plan tool mask = %v", plan.ToolNames)
	}
	before := regular.ToolRevision
	if err := registry.RegisterDefinition(&engineNamedTool{name: "new_read", effect: tool.SideEffectRead}); err != nil {
		t.Fatal(err)
	}
	changed, err := runtime.CaptureStep(host.Snapshot, turn.TurnContext{Mode: turn.ModeKindDefault})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ToolRevision == before || !containsName(changed.ToolNames, "new_read") {
		t.Fatalf("dynamic StepContext was stale: before=%q changed=%q tools=%v", before, changed.ToolRevision, changed.ToolNames)
	}
}

func TestTurnEngineWarnsThenReturnsTypedBlockedAtSafetyBudget(t *testing.T) {
	toolResponse := func(id string) []llm.StreamChunk {
		return []llm.StreamChunk{{ToolCalls: []llm.ToolCall{{ID: id, Name: "unstable", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls}}
	}
	client := &engineTestClient{streams: [][]llm.StreamChunk{toolResponse("call-1"), toolResponse("call-2")}}
	registry := tool.NewRegistry()
	if err := registry.RegisterDefinition(&engineTestTool{}); err != nil {
		t.Fatal(err)
	}
	service, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{MaxParallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Services{
		providerName: "test", provider: config.ProviderConfig{Model: "test-model", MaxOutputTokens: 1024}, client: client,
		baseInstructions: llm.BaseInstructions{Text: "test instructions"}, registry: registry,
		toolService: service, visibility: map[string]bool{},
		budget: TurnBudget{MaxSamples: 2, MaxToolCalls: 100, MaxDuration: time.Hour, WarnRatio: 0.5},
	}
	host := &engineTestHost{context: agentcontext.NewManager(nil)}
	user, _ := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: "keep trying"})
	if err := host.AppendItems(context.Background(), "turn-budget", user); err != nil {
		t.Fatal(err)
	}
	result, err := RunTurn(context.Background(), runtime, RunRequest{
		Snapshot: host.Snapshot, AppendItems: host.AppendItems, Events: host, Instructions: &engineTestScope{},
		Turn: turn.TurnContext{ThreadID: "thread-1", TurnID: "turn-budget", Provider: "test", Model: "test-model", CWD: t.TempDir(), Mode: turn.ModeKindDefault},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != rollout.TurnOutcomeBlocked || !strings.Contains(result.Reason, "sample safety limit") {
		t.Fatalf("budget result = %#v", result)
	}
	if len(client.requests) != 2 || !strings.Contains(requestText(client.requests[1]), "approaching its internal safety budget") {
		t.Fatalf("completion reminder missing: %#v", client.requests)
	}
}

func TestTurnEngineChecksAutomaticCompactionBeforeSampling(t *testing.T) {
	client := &engineTestClient{streams: [][]llm.StreamChunk{{{ContentDelta: "done"}, {FinishReason: llm.FinishReasonStop}}}}
	registry := tool.NewRegistry()
	service, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Services{
		providerName: "test", provider: config.ProviderConfig{Model: "test-model", ContextWindow: 1_000, AutoCompactTokenLimit: 1, MaxOutputTokens: 64}, client: client,
		baseInstructions: llm.BaseInstructions{Text: "test instructions"}, registry: registry,
		toolService: service, visibility: map[string]bool{}, budget: DefaultTurnBudget(),
	}
	host := &engineTestHost{context: agentcontext.NewManager(nil)}
	user, _ := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: "a context that exceeds one token"})
	if err := host.AppendItems(context.Background(), "turn-compact", user); err != nil {
		t.Fatal(err)
	}
	checks := 0
	result, err := RunTurn(context.Background(), runtime, RunRequest{
		Snapshot: host.Snapshot, AppendItems: host.AppendItems, Events: host, Instructions: &engineTestScope{},
		Turn: turn.TurnContext{ThreadID: "thread-1", TurnID: "turn-compact", Provider: "test", Model: "test-model", CWD: t.TempDir(), Mode: turn.ModeKindDefault},
		Compact: func(context.Context) (bool, error) {
			checks++
			return false, nil
		},
	})
	if err != nil || result.Outcome != rollout.TurnOutcomeCompleted || checks != 1 || len(client.requests) != 1 {
		t.Fatalf("auto compaction check result=%#v checks=%d requests=%d err=%v", result, checks, len(client.requests), err)
	}
}

func requestText(request llm.Request) string {
	parts := make([]string, 0, len(request.Prompt.Input))
	for _, message := range request.Prompt.Input {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}

func containsName(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func assertBefore(t *testing.T, values []string, left, right string) {
	t.Helper()
	leftIndex, rightIndex := firstIndex(values, left), firstIndex(values, right)
	if leftIndex < 0 || rightIndex < 0 || leftIndex > rightIndex {
		t.Fatalf("expected %q before %q: %v", left, right, values)
	}
}

func firstIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func lastIndex(values []string, target string) int {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] == target {
			return index
		}
	}
	return -1
}
