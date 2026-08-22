package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/Godric-W/Amadeus/internal/thread/local"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type continuationTestClient struct {
	mu       sync.Mutex
	requests []llm.Request
	streams  []llm.Stream
	messages llm.ModelMessages
}

func (*continuationTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected complete call")
}

func (client *continuationTestClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.requests = append(client.requests, request)
	if len(client.streams) == 0 {
		return nil, errors.New("no scripted stream")
	}
	stream := client.streams[0]
	client.streams = client.streams[1:]
	return stream, nil
}

func (client *continuationTestClient) Model() llm.ModelInfo {
	return continuationModelInfo(client.messages)
}

func (*continuationTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsParallelToolCalls: true}
}

type continuationStreamResult struct {
	chunk llm.StreamChunk
	err   error
}

type continuationTestStream struct {
	results []continuationStreamResult
}

func (stream *continuationTestStream) Recv() (llm.StreamChunk, error) {
	if len(stream.results) == 0 {
		return llm.StreamChunk{}, io.EOF
	}
	result := stream.results[0]
	stream.results = stream.results[1:]
	return result.chunk, result.err
}

func (*continuationTestStream) Close() error { return nil }

func continuationStream(chunks ...llm.StreamChunk) llm.Stream {
	results := make([]continuationStreamResult, len(chunks))
	for index, chunk := range chunks {
		results[index] = continuationStreamResult{chunk: chunk}
	}
	return &continuationTestStream{results: results}
}

type continuationFailingTool struct{}

func (*continuationFailingTool) Spec() tool.ToolSpec {
	return tool.ToolSpec{Name: "unstable", Description: "always fails", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}

func (*continuationFailingTool) SupportsParallelToolCalls() bool { return true }

func (*continuationFailingTool) ValidateInput(tool.ToolUseContext, tool.Invocation) error {
	return nil
}

func (*continuationFailingTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return tool.PreparedToolUse{Invocation: invocation, Permission: tool.AllowPermission()}, nil
}

func (*continuationFailingTool) Execute(tool.ToolUseContext, tool.PreparedToolUse) (tool.ToolResult, error) {
	return tool.ToolResult{}, errors.New("temporary failure")
}

type continuationNamedTool struct {
	name   string
	effect tool.SideEffect
}

type continuationLargeTool struct {
	name  string
	text  string
	after func()
}

func (definition *continuationLargeTool) Spec() tool.ToolSpec {
	name := definition.name
	if name == "" {
		name = "large_output"
	}
	return tool.ToolSpec{Name: name, Description: "returns a test result", InputSchema: json.RawMessage(`{"type":"object"}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}

func (*continuationLargeTool) SupportsParallelToolCalls() bool { return true }

func (*continuationLargeTool) ValidateInput(tool.ToolUseContext, tool.Invocation) error { return nil }

func (*continuationLargeTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return tool.PreparedToolUse{Invocation: invocation, Permission: tool.AllowPermission()}, nil
}

func (definition *continuationLargeTool) Execute(_ tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	if definition.after != nil {
		definition.after()
	}
	text := definition.text
	if text == "" {
		text = strings.Repeat("x", 5000)
	}
	return tool.ToolResult{CallID: prepared.Invocation.Call.ID, ToolName: prepared.Invocation.Call.Name, Text: text}, nil
}

func (definition *continuationNamedTool) Spec() tool.ToolSpec {
	return tool.ToolSpec{Name: definition.name, Description: "named test tool", InputSchema: json.RawMessage(`{"type":"object"}`), SideEffect: definition.effect, Idempotent: true}
}

func (*continuationNamedTool) SupportsParallelToolCalls() bool { return true }

func (*continuationNamedTool) ValidateInput(tool.ToolUseContext, tool.Invocation) error {
	return nil
}

func (*continuationNamedTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return tool.PreparedToolUse{Invocation: invocation, Permission: tool.AllowPermission()}, nil
}

func (*continuationNamedTool) Execute(_ tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	return tool.ToolResult{CallID: prepared.Invocation.Call.ID, ToolName: prepared.Invocation.Call.Name}, nil
}

type continuationEventSink struct {
	session *Session
	events  []protocol.Event
}

func (sink *continuationEventSink) Publish(ctx context.Context, event protocol.Event) error {
	if completed, ok := event.Msg.(protocol.ItemCompletedEvent); ok {
		history, err := sink.session.services.LiveThread.History(ctx)
		if err != nil {
			return err
		}
		persisted := false
		for _, line := range history.Lines {
			item, itemOK := line.Item.(rollout.EventMsgItem)
			if !itemOK {
				continue
			}
			value, eventOK := item.Msg.(protocol.ItemCompletedEvent)
			if eventOK && value.Item.ID == completed.Item.ID {
				persisted = true
				break
			}
		}
		if !persisted {
			return errors.New("item completion was published before canonical persistence")
		}
	}
	sink.events = append(sink.events, event)
	return nil
}

func TestContinueTurnPersistsAndContinuesAfterToolFailure(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "unstable", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls, Usage: &llm.Usage{TotalTokens: 10}}),
		continuationStream(llm.StreamChunk{ContentDelta: "recovered"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop, Usage: &llm.Usage{TotalTokens: 5}}),
	}}
	session := newContinuationTestSession(t, client, []tool.ToolDefinition{&continuationFailingTool{}}, continuationModelInfo(llm.ModelMessages{}), continuationProvider(0), engine.DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-1", "do work")
	events := &continuationEventSink{session: session}
	result, err := runContinuationTestTurn(session, "turn-1", events, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != protocol.TurnOutcomeCompleted || result.Usage.TotalTokens != 15 || len(client.requests) != 2 {
		t.Fatalf("turn result=%#v requests=%d", result, len(client.requests))
	}
	secondInput := client.requests[1].Prompt.Input
	if len(secondInput) == 0 || secondInput[len(secondInput)-1].Role != llm.RoleTool || !strings.Contains(secondInput[len(secondInput)-1].Content, "temporary failure") {
		t.Fatalf("tool failure was not returned to model: %#v", secondInput)
	}
}

func TestContinueTurnKeepsFrozenReasoningEffortAcrossContinuations(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "change_config", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls}),
		continuationStream(llm.StreamChunk{ContentDelta: "done"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	high := llm.ReasoningEffortHigh
	low := llm.ReasoningEffortLow
	changingTool := &continuationLargeTool{name: "change_config", text: "changed"}
	session := newContinuationTestSession(t, client, []tool.ToolDefinition{changingTool}, continuationModelInfo(llm.ModelMessages{}), continuationProvider(0), engine.DefaultTurnBudget())
	session.state.Configuration.Runtime.ModelReasoningEffort = &high
	changingTool.after = func() { session.state.Configuration.Runtime.ModelReasoningEffort = &low }
	appendContinuationUser(t, session, "turn-effort", "do work")
	if _, err := runContinuationTestTurn(session, "turn-effort", &continuationEventSink{session: session}, nil); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	for index, request := range client.requests {
		if request.Reasoning == nil || request.Reasoning.Effort == nil || *request.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("request %d effort = %#v, want high", index, request.Reasoning)
		}
	}
}

func TestCaptureStepUsesOneFrozenRouterForModeAndRegistryRevision(t *testing.T) {
	client := &continuationTestClient{}
	definitions := []tool.ToolDefinition{
		&continuationNamedTool{name: "read", effect: tool.SideEffectRead},
		&continuationNamedTool{name: "write", effect: tool.SideEffectWrite},
		&continuationNamedTool{name: "update_plan", effect: tool.SideEffectNone},
		&continuationNamedTool{name: "web_search", effect: tool.SideEffectNetwork},
	}
	session := newContinuationTestSession(t, client, definitions, continuationModelInfo(llm.ModelMessages{}), continuationProvider(0), engine.DefaultTurnBudget())
	regular, err := session.services.CaptureStep(session.Snapshot, continuationTurnContext(session, "turn-regular", turn.ModeKindDefault))
	if err != nil {
		t.Fatal(err)
	}
	if !containsContinuationName(regular.ToolRouter.Names(), "update_plan") || !containsContinuationName(regular.ToolRouter.Names(), "write") {
		t.Fatalf("regular tool mask = %v", regular.ToolRouter.Names())
	}
	planStep, err := session.services.CaptureStep(session.Snapshot, continuationTurnContext(session, "turn-plan", turn.ModeKindPlan))
	if err != nil {
		t.Fatal(err)
	}
	if containsContinuationName(planStep.ToolRouter.Names(), "update_plan") || containsContinuationName(planStep.ToolRouter.Names(), "write") || !containsContinuationName(planStep.ToolRouter.Names(), "read") || !containsContinuationName(planStep.ToolRouter.Names(), "web_search") {
		t.Fatalf("plan tool mask = %v", planStep.ToolRouter.Names())
	}
	before := regular.ToolRouter.Revision()
	if err := session.services.tools.RegisterDefinition(&continuationNamedTool{name: "new_read", effect: tool.SideEffectRead}); err != nil {
		t.Fatal(err)
	}
	changed, err := session.services.CaptureStep(session.Snapshot, continuationTurnContext(session, "turn-changed", turn.ModeKindDefault))
	if err != nil {
		t.Fatal(err)
	}
	if changed.ToolRouter.Revision() == before || !containsContinuationName(changed.ToolRouter.Names(), "new_read") {
		t.Fatalf("new StepContext did not capture registry change: before=%q changed=%q tools=%v", before, changed.ToolRouter.Revision(), changed.ToolRouter.Names())
	}
	if containsContinuationName(regular.ToolRouter.Names(), "new_read") {
		t.Fatalf("frozen router changed after registry mutation: %v", regular.ToolRouter.Names())
	}
}

func TestContinueTurnWarnsThenReturnsBlockedAtSafetyBudget(t *testing.T) {
	toolResponse := func(id string) llm.Stream {
		return continuationStream(llm.StreamChunk{ToolCalls: []llm.ToolCall{{ID: id, Name: "unstable", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls})
	}
	client := &continuationTestClient{streams: []llm.Stream{toolResponse("call-1"), toolResponse("call-2")}}
	budget := engine.TurnBudget{MaxSamples: 2, MaxToolCalls: 100, MaxDuration: time.Hour, WarnRatio: 0.5}
	session := newContinuationTestSession(t, client, []tool.ToolDefinition{&continuationFailingTool{}}, continuationModelInfo(llm.ModelMessages{}), continuationProvider(0), budget)
	appendContinuationUser(t, session, "turn-budget", "keep trying")
	result, err := runContinuationTestTurn(session, "turn-budget", &continuationEventSink{session: session}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != protocol.TurnOutcomeBlocked || !strings.Contains(result.Reason, "sample safety limit") {
		t.Fatalf("budget result = %#v", result)
	}
	if len(client.requests) != 2 || !strings.Contains(continuationRequestText(client.requests[1]), "approaching its internal safety budget") {
		t.Fatalf("completion reminder missing: %#v", client.requests)
	}
}

func TestContinueTurnChecksAutomaticCompactionBeforeSampling(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ContentDelta: "done"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(llm.ModelMessages{})
	model.ContextWindow = 1_000
	model.AutoCompactTokenLimit = 1
	session := newContinuationTestSession(t, client, nil, model, continuationProvider(0), engine.DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-compact", "a context that exceeds one token")
	checks := 0
	compact := func(context.Context) (bool, error) {
		checks++
		return false, nil
	}
	result, err := runContinuationTestTurn(session, "turn-compact", &continuationEventSink{session: session}, compact)
	if err != nil || result.Outcome != protocol.TurnOutcomeCompleted || checks != 1 || len(client.requests) != 1 {
		t.Fatalf("auto compaction result=%#v checks=%d requests=%d err=%v", result, checks, len(client.requests), err)
	}
}

func TestSteeredInputFollowsCompactionWhenOnlySteerNeedsFollowUp(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ContentDelta: strings.Repeat("a", 5000)}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
		continuationStream(llm.StreamChunk{ContentDelta: "processed steer"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(llm.ModelMessages{})
	model.AutoCompactTokenLimit = 200
	session := newContinuationTestSession(t, client, nil, model, continuationProvider(0), engine.DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-steer-compact", "first prompt")
	state := newTurnState()
	if err := session.inputQueue.Enqueue(state, UserTurnInput{Content: "second prompt", ClientID: "client-2"}); err != nil {
		t.Fatal(err)
	}
	compactChecks := make([]string, 0, 1)
	compact := func(context.Context) (bool, error) {
		projection := session.ContextProjection()
		parts := make([]string, len(projection.Messages))
		for index, message := range projection.Messages {
			parts[index] = message.Content
		}
		compactChecks = append(compactChecks, strings.Join(parts, "\n"))
		session.services.modelInfo.AutoCompactTokenLimit = 100_000
		return true, nil
	}
	result, err := runContinuationWithState(session, "turn-steer-compact", state, &continuationEventSink{session: session}, compact, false)
	if err != nil || result.Outcome != protocol.TurnOutcomeCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(compactChecks) != 1 || strings.Contains(compactChecks[0], "second prompt") {
		t.Fatalf("compact inputs = %#v", compactChecks)
	}
	if len(client.requests) != 2 || strings.Contains(continuationRequestText(client.requests[0]), "second prompt") || !strings.Contains(continuationRequestText(client.requests[1]), "second prompt") {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestSteeredInputWaitsForPostCompactToolContinuation(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "large_output", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls}),
		continuationStream(llm.StreamChunk{ContentDelta: "resumed after compact"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
		continuationStream(llm.StreamChunk{ContentDelta: "processed steer"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(llm.ModelMessages{})
	model.AutoCompactTokenLimit = 100_000
	largeTool := &continuationLargeTool{}
	session := newContinuationTestSession(t, client, []tool.ToolDefinition{largeTool}, model, continuationProvider(0), engine.DefaultTurnBudget())
	largeTool.after = func() { session.services.modelInfo.AutoCompactTokenLimit = 200 }
	appendContinuationUser(t, session, "turn-tool-compact", "first prompt")
	state := newTurnState()
	if err := session.inputQueue.Enqueue(state, UserTurnInput{Content: "second prompt", ClientID: "client-2"}); err != nil {
		t.Fatal(err)
	}
	compactCalls := 0
	compact := func(context.Context) (bool, error) {
		compactCalls++
		if strings.Contains(contextProjectionText(session.ContextProjection().Messages), "second prompt") {
			t.Fatal("steered input entered compact request")
		}
		session.services.modelInfo.AutoCompactTokenLimit = 100_000
		return true, nil
	}
	result, err := runContinuationWithState(session, "turn-tool-compact", state, &continuationEventSink{session: session}, compact, false)
	if err != nil || result.Outcome != protocol.TurnOutcomeCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if compactCalls != 1 || len(client.requests) != 3 {
		t.Fatalf("compact calls=%d requests=%d", compactCalls, len(client.requests))
	}
	if strings.Contains(continuationRequestText(client.requests[1]), "second prompt") || !strings.Contains(continuationRequestText(client.requests[2]), "second prompt") {
		t.Fatalf("request ordering = %#v", client.requests)
	}
}

func TestSteeredInputWaitsForModelContinuationAfterMidTurnCompact(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		continuationStream(llm.StreamChunk{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "compact_trigger", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls}),
		continuationStream(llm.StreamChunk{ContentDelta: "resumed old task"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
		continuationStream(llm.StreamChunk{ContentDelta: "processed steer"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	model := continuationModelInfo(llm.ModelMessages{})
	model.AutoCompactTokenLimit = 100_000
	trigger := &continuationLargeTool{name: "compact_trigger", text: "ok"}
	session := newContinuationTestSession(t, client, []tool.ToolDefinition{trigger}, model, continuationProvider(0), engine.DefaultTurnBudget())
	trigger.after = func() { session.services.modelInfo.AutoCompactTokenLimit = 1 }
	appendContinuationUser(t, session, "turn-mid-compact", "first prompt")
	state := newTurnState()
	if err := session.inputQueue.Enqueue(state, UserTurnInput{Content: "second prompt", ClientID: "client-2"}); err != nil {
		t.Fatal(err)
	}
	compactCalls := 0
	compact := func(context.Context) (bool, error) {
		compactCalls++
		session.services.modelInfo.AutoCompactTokenLimit = 100_000
		return true, nil
	}
	result, err := runContinuationWithState(session, "turn-mid-compact", state, &continuationEventSink{session: session}, compact, false)
	if err != nil || result.Outcome != protocol.TurnOutcomeCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if compactCalls != 1 || len(client.requests) != 3 {
		t.Fatalf("compact calls=%d requests=%d", compactCalls, len(client.requests))
	}
	if strings.Contains(continuationRequestText(client.requests[1]), "second prompt") || !strings.Contains(continuationRequestText(client.requests[2]), "second prompt") {
		t.Fatalf("request ordering = %#v", client.requests)
	}
}

func TestContinueTurnRetryPersistsOnlySuccessfulAttempt(t *testing.T) {
	client := &continuationTestClient{streams: []llm.Stream{
		&continuationTestStream{results: []continuationStreamResult{
			{chunk: llm.StreamChunk{ContentDelta: "partial"}},
			{err: &llm.ProviderError{Kind: llm.ProviderErrorNetwork, Message: "reset", Retryable: true, RetryDelay: time.Nanosecond}},
		}},
		continuationStream(llm.StreamChunk{ContentDelta: "recovered"}, llm.StreamChunk{FinishReason: llm.FinishReasonStop}),
	}}
	session := newContinuationTestSession(t, client, nil, continuationModelInfo(llm.ModelMessages{}), continuationProvider(1), engine.DefaultTurnBudget())
	appendContinuationUser(t, session, "turn-retry", "continue")
	result, err := runContinuationTestTurn(session, "turn-retry", &continuationEventSink{session: session}, nil)
	if err != nil || result.Outcome != protocol.TurnOutcomeCompleted {
		t.Fatalf("run result=%#v err=%v", result, err)
	}
	history, err := session.services.LiveThread.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	projection, err := agentcontext.ProjectRolloutMessages(history.Lines)
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

func TestSessionServicesPreferModelMessagesFromCurrentModel(t *testing.T) {
	catalog := continuationModelMessages(t)
	modelMessages := catalog
	modelMessages.InstructionsTemplate = "model-specific {{ personality }}"
	modelMessages.Revision = "model-specific-revision"
	services := SessionServices{modelInfo: continuationModelInfo(modelMessages), modelMessages: catalog}
	resolved, err := services.ModelMessages(services.ModelInfo())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision != "model-specific-revision" || !strings.HasPrefix(resolved.InstructionsTemplate, "model-specific") {
		t.Fatalf("current model messages were not selected: %#v", resolved)
	}
}

func newContinuationTestSession(t *testing.T, client llm.Client, definitions []tool.ToolDefinition, model llm.ModelInfo, provider config.ModelProviderInfo, budget engine.TurnBudget) *Session {
	t.Helper()
	ctx := context.Background()
	home := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	database, err := statesqlite.Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	stateStore, err := statesqlite.NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	threadStore, err := local.NewStore(home, stateStore, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	live, err := thread.NewDraftLiveThread(testutil.ThreadID(1), threadStore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.Materialize(ctx, thread.CreateInput{SessionID: testutil.SessionID(1), CWD: t.TempDir(), Title: "continuation test", ModelProvider: model.Provider, Model: model.Name, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	history, err := live.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agentcontext.NewManagerFromRollout(history.Lines, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	for _, definition := range definitions {
		if err := registry.RegisterDefinition(definition); err != nil {
			t.Fatal(err)
		}
	}
	executor, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{MaxParallel: 2})
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancelCause(context.Background())
	session := newTestSession(history.Lines, manager)
	session.sessionID = testutil.SessionID(1)
	session.threadID = testutil.ThreadID(1)
	session.ctx = parent
	session.cancel = cancel
	session.services = SessionServices{
		LiveThread: live, Clock: func() time.Time { return now }, NextID: func(kind string) string { return kind + "-test" },
		modelClient: client, provider: provider, modelInfo: model, modelMessages: continuationModelMessages(t),
		tools: registry, toolExecutor: executor, visibility: map[string]bool{}, budget: budget,
	}
	t.Cleanup(func() {
		cancel(errors.New("test cleanup"))
		_ = live.Shutdown(context.Background())
		_ = threadStore.Close()
		_ = database.Close()
	})
	return session
}

func appendContinuationUser(t *testing.T, session *Session, turnID protocol.TurnID, content string) {
	t.Helper()
	user, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendItems(context.Background(), turnID, user); err != nil {
		t.Fatal(err)
	}
}

func runContinuationTestTurn(session *Session, turnID protocol.TurnID, events protocol.EventSink, compact compactFunc) (TaskOutput, error) {
	return runContinuationWithState(session, turnID, newTurnState(), events, compact, false)
}

func runContinuationWithState(session *Session, turnID protocol.TurnID, state *TurnState, events protocol.EventSink, compact compactFunc, canDrainPendingInput bool) (TaskOutput, error) {
	modelSession, err := session.services.NewModelClientSession()
	if err != nil {
		return TaskOutput{}, err
	}
	return session.continueTurn(context.Background(), &session.services, modelSession, continuationTurnContext(session, turnID, turn.ModeKindDefault), state, events, compact, canDrainPendingInput)
}

func contextProjectionText(messages []llm.ResponseItem) string {
	parts := make([]string, len(messages))
	for index, message := range messages {
		parts[index] = message.Content
	}
	return strings.Join(parts, "\n")
}

func continuationTurnContext(session *Session, turnID protocol.TurnID, mode turn.ModeKind) turn.TurnContext {
	return turn.TurnContext{
		SessionID: session.sessionID, ThreadID: session.threadID, TurnID: turnID,
		Provider: session.services.modelInfo.Provider, Model: session.services.modelInfo.Name,
		ReasoningEffort: llm.CloneReasoningEffort(session.state.Configuration.Runtime.ModelReasoningEffort),
		CWD:             "/workspace", Mode: mode,
	}
}

func continuationProvider(streamRetries int) config.ModelProviderInfo {
	return config.ModelProviderInfo{StreamMaxRetries: streamRetries, StreamIdleTimeout: time.Second}
}

func continuationModelInfo(messages llm.ModelMessages) llm.ModelInfo {
	return llm.ModelInfo{
		Provider: "test", Name: "test-model", ContextWindow: 100_000,
		AutoCompactTokenLimit: 90_000, ToolOutputTokenLimit: 10_000,
		SupportsParallelToolCalls: true, ModelMessages: messages,
	}
}

func continuationModelMessages(t *testing.T) llm.ModelMessages {
	t.Helper()
	messages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func continuationRequestText(request llm.Request) string {
	parts := make([]string, 0, len(request.Prompt.Input))
	for _, message := range request.Prompt.Input {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}

func containsContinuationName(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
