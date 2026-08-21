package session

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type interactiveCompactionClient struct {
	request  llm.Request
	requests []llm.Request
	streams  []llm.Stream
	err      error
}

func (client *interactiveCompactionClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected complete call")
}

func (client *interactiveCompactionClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.request = request
	client.requests = append(client.requests, request)
	if client.err != nil {
		return nil, client.err
	}
	if len(client.streams) > 0 {
		stream := client.streams[0]
		client.streams = client.streams[1:]
		return stream, nil
	}
	return successfulCompactStream(), nil
}

func successfulCompactStream() llm.Stream {
	return &compactTestStream{chunks: []llm.StreamChunk{
		{ContentDelta: "## Handoff\n\nInspection completed; continue with tests."},
		{FinishReason: llm.FinishReasonStop},
	}}
}

func (*interactiveCompactionClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "compact-model"}
}

func (*interactiveCompactionClient) Capabilities() llm.Capabilities { return llm.Capabilities{} }

type compactTestStream struct {
	chunks []llm.StreamChunk
	err    error
}

func (stream *compactTestStream) Recv() (llm.StreamChunk, error) {
	if len(stream.chunks) == 0 {
		if stream.err != nil {
			err := stream.err
			stream.err = nil
			return llm.StreamChunk{}, err
		}
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[0]
	stream.chunks = stream.chunks[1:]
	return chunk, nil
}

func (*compactTestStream) Close() error { return nil }

type compactTestHost struct {
	lines   []rollout.Line
	context *agentcontext.Manager
	events  []protocol.Event
}

func (host *compactTestHost) AppendItems(_ context.Context, turnID protocol.TurnID, items ...rollout.RolloutItem) error {
	for _, item := range items {
		scoped := rollout.ScopeItem(item, "thread-1", turnID)
		host.lines = append(host.lines, rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(len(host.lines) + 1), Timestamp: time.Now().UTC(), Item: scoped})
	}
	return host.context.Rebuild(host.lines)
}

func (host *compactTestHost) History() []rollout.Line {
	return rollout.CloneLines(host.lines)
}
func (host *compactTestHost) Publish(_ context.Context, event protocol.Event) error {
	host.events = append(host.events, event)
	return nil
}
func (*compactTestHost) Request(context.Context, protocol.ApprovalRequestEvent) (protocol.Op, error) {
	return nil, errors.New("unexpected interactive request")
}
func (host *compactTestHost) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	return host.context.Snapshot(model, prompt)
}
func (host *compactTestHost) ContextUpdate(key agentcontext.UpdateKey) string {
	return host.context.Update(key)
}

func TestCompactTaskProducesSemanticReplacementHistory(t *testing.T) {
	session, host, client := newCompactionTestRuntime(t)
	runtime := &session.services
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(client.request.Prompt.Input) < 3 || !strings.Contains(client.request.Prompt.BaseInstructions.Text, "CONTEXT CHECKPOINT COMPACTION") {
		t.Fatalf("compaction request was incomplete: %#v", client.request.Prompt)
	}
	if len(result.Items) != 2 {
		t.Fatalf("compaction result = %#v", result)
	}
	if _, ok := result.Items[0].(rollout.CompactedItem); !ok {
		t.Fatalf("compaction item = %T", result.Items[0])
	}
	usageItem, ok := result.Items[1].(rollout.EventMsgItem)
	if !ok {
		t.Fatalf("usage item = %T", result.Items[1])
	}
	if _, ok := usageItem.Msg.(protocol.TokenCountEvent); !ok {
		t.Fatalf("compaction result = %#v", result)
	}
	if err := host.AppendItems(context.Background(), "turn-2", result.Items...); err != nil {
		t.Fatal(err)
	}
	projection, err := agentcontext.ProjectRolloutMessages(host.History())
	if err != nil || len(projection.Messages) != 2 || projection.Messages[0].Role != llm.RoleUser || !strings.Contains(projection.Messages[1].Content, "Another language model started to solve this problem") || !strings.Contains(projection.Messages[1].Content, "Inspection completed") {
		t.Fatalf("replacement history was not authoritative: projection=%#v err=%v", projection, err)
	}
}

func TestCompactTaskUsesFrozenReasoningEffort(t *testing.T) {
	session, host, client := newCompactionTestRuntime(t)
	effort := llm.ReasoningEffortXHigh
	turnContext := compactTurnContext()
	turnContext.ReasoningEffort = &effort
	if _, err := (&compactTask{runtime: &session.services, events: host}).Run(context.Background(), session, turnContext); err != nil {
		t.Fatal(err)
	}
	assertRequestReasoningEffort(t, client.request, effort)
}

func TestCompactorUsesProvidedReasoningEffort(t *testing.T) {
	session, host, client := newCompactionTestRuntime(t)
	modelSession, err := session.services.NewModelClientSession()
	if err != nil {
		t.Fatal(err)
	}
	effort := llm.ReasoningEffortHigh
	items, err := session.services.Compact(context.Background(), engine.CompactRequest{
		History: session.ContextProjection(), ModelSession: modelSession,
		Reasoning: llm.ReasoningConfigForEffort(&effort), Events: host,
	})
	if err != nil || len(items) == 0 {
		t.Fatalf("compaction items=%d err=%v", len(items), err)
	}
	assertRequestReasoningEffort(t, client.request, effort)
}

func TestCompactTaskUsesEffectiveToolOutputTokenLimit(t *testing.T) {
	session, host, client := newCompactionTestRuntimeWithOptions(t, 5, 40)
	toolCall, err := rollout.NewResponseItem(rollout.ResponseItem{
		Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "read", Arguments: []byte(`{"path":"large.txt"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	result := &tool.ToolResult{CallID: "call-1", ToolName: "read", Text: strings.Repeat("large output ", 200)}
	toolResult, err := rollout.NewResponseItem(rollout.ResponseItem{
		Type: rollout.ResponseToolResult, Role: "tool", CallID: "call-1", Name: "read", Status: "succeeded", Result: result,
	})
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := rollout.NewResponseItem(rollout.ResponseItem{
		Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "The large file was inspected.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-1", toolCall, toolResult, assistant); err != nil {
		t.Fatal(err)
	}
	runtime := &session.services
	if _, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext()); err != nil {
		t.Fatal(err)
	}
	for _, item := range client.request.Prompt.Input {
		if item.Role == llm.RoleTool {
			if !strings.Contains(item.Content, "truncated for the model") {
				t.Fatalf("compaction Tool Result bypassed effective token limit: %q", item.Content)
			}
			return
		}
	}
	t.Fatal("compaction request did not include the projected Tool Result")
}

func TestCompactTaskPreservesLatestUserTurnOutsideReplacement(t *testing.T) {
	session, host, _ := newCompactionTestRuntime(t)
	runtime := &session.services
	latest, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "now run the tests"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-2", latest); err != nil {
		t.Fatal(err)
	}
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-2", result.Items...); err != nil {
		t.Fatal(err)
	}
	projection, err := agentcontext.ProjectRolloutMessages(host.History())
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Messages) != 3 || projection.Messages[0].Content != "inspect project" || !strings.Contains(projection.Messages[1].Content, "Another language model started to solve this problem") || projection.Messages[2].Content != "now run the tests" {
		t.Fatalf("latest user turn was not preserved: %#v", projection.Messages)
	}
}

func TestCompactTaskFailureDoesNotReturnItems(t *testing.T) {
	session, host, client := newCompactionTestRuntime(t)
	runtime := &session.services
	client.err = errors.New("provider unavailable")
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext())
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("failed compaction result=%#v err=%v", result, err)
	}
}

func TestCompactTaskRetriesResponseStreamAndKeepsCanonicalResult(t *testing.T) {
	session, host, client := newCompactionTestRuntimeWithRetries(t, 1)
	client.streams = []llm.Stream{compactRetryFailure("connection reset"), successfulCompactStream()}
	runtime := &session.services
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext())
	if err != nil {
		t.Fatal(err)
	}
	_, compacted := result.Items[0].(rollout.CompactedItem)
	if len(client.requests) != 2 || len(result.Items) != 2 || !compacted {
		t.Fatalf("retry compaction requests=%d result=%#v", len(client.requests), result)
	}
	if retrying, terminal := compactStreamErrorCounts(host.events); retrying != 1 || terminal != 0 {
		t.Fatalf("retry compaction events retrying=%d terminal=%d events=%#v", retrying, terminal, host.events)
	}
}

func TestCompactTaskRetryExhaustionDoesNotChangeReplacementHistory(t *testing.T) {
	session, host, client := newCompactionTestRuntimeWithRetries(t, 1)
	client.streams = []llm.Stream{compactRetryFailure("first failure"), compactRetryFailure("second failure")}
	original := session.ContextProjection()
	runtime := &session.services
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext())
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("exhausted compaction result=%#v err=%v", result, err)
	}
	if current := session.ContextProjection(); !reflect.DeepEqual(current, original) {
		t.Fatalf("failed compaction changed history projection: before=%#v after=%#v", original, current)
	}
	if retrying, terminal := compactStreamErrorCounts(host.events); retrying != 1 || terminal != 1 {
		t.Fatalf("exhausted compaction events retrying=%d terminal=%d events=%#v", retrying, terminal, host.events)
	}
}

func TestCompactionSuccessEventOrderRemainsContextWarningTerminal(t *testing.T) {
	item := rollout.CompactedItem{
		ThreadID: "thread-1", TurnID: "turn-1",
		Summary: "summary", ReplacementHistory: []rollout.ReplacementMessage{{Role: "assistant", Content: "summary"}},
		CoveredThroughSequence: 1, SourceHash: "hash", Provider: "mock", Model: "compact-model",
	}
	session := &Session{threadID: "thread-1", ctx: context.Background(), events: make(chan protocol.Event, 3)}
	session.publishCompactionEvents("submission-1", "turn-1", []rollout.RolloutItem{item})
	session.publish(protocol.Event{ID: "submission-1", Msg: protocol.TurnCompleteEvent{ThreadID: "thread-1", TurnID: "turn-1"}})

	first := <-session.events
	second := <-session.events
	third := <-session.events
	if _, ok := first.Msg.(protocol.ContextCompactedEvent); !ok {
		t.Fatalf("first event = %T", first.Msg)
	}
	if warning, ok := second.Msg.(protocol.WarningEvent); !ok || warning.Message != compactionWarningMessage {
		t.Fatalf("second event = %#v", second.Msg)
	}
	if _, ok := third.Msg.(protocol.TurnCompleteEvent); !ok {
		t.Fatalf("third event = %T", third.Msg)
	}
}

func compactTurnContext() *turn.TurnContext {
	return &turn.TurnContext{ThreadID: "thread-1", TurnID: "turn-2"}
}

func assertRequestReasoningEffort(t *testing.T, request llm.Request, want llm.ReasoningEffort) {
	t.Helper()
	if request.Reasoning == nil || request.Reasoning.Effort == nil || *request.Reasoning.Effort != want {
		t.Fatalf("request reasoning = %#v, want %q", request.Reasoning, want)
	}
}

func newCompactionTestRuntime(t *testing.T) (*Session, *compactTestHost, *interactiveCompactionClient) {
	return newCompactionTestRuntimeWithRetries(t, 5)
}

func newCompactionTestRuntimeWithRetries(t *testing.T, streamMaxRetries int) (*Session, *compactTestHost, *interactiveCompactionClient) {
	return newCompactionTestRuntimeWithOptions(t, streamMaxRetries, 10_000)
}

func newCompactionTestRuntimeWithOptions(t *testing.T, streamMaxRetries, toolOutputTokenLimit int) (*Session, *compactTestHost, *interactiveCompactionClient) {
	t.Helper()
	client := &interactiveCompactionClient{}
	configured := config.Default()
	configured.Model = "compact-model"
	configured.ModelProvider = "mock"
	configured.ModelContextWindow = 8_192
	configured.ToolOutputTokenLimit = int64(toolOutputTokenLimit)
	configured.ModelProviders = map[string]config.ModelProviderInfo{
		"mock": {
			WireAPI:           config.WireAPIResponses,
			Dialect:           config.DialectStandard,
			APIKey:            "test-key",
			BaseURL:           "https://example.invalid/v1",
			Timeout:           2 * time.Minute,
			RequestMaxRetries: 4,
			StreamMaxRetries:  streamMaxRetries,
			StreamIdleTimeout: 5 * time.Minute,
		},
	}
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	adapters := ServiceAdapters{
		ModelMessages: mustLoadModelMessages(t),
		ClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
		AuditFactory: func() (audit.Sink, io.Closer, error) {
			return audit.NewMemorySink(), io.NopCloser(strings.NewReader("")), nil
		},
	}
	user, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect project"})
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "inspection completed"})
	if err != nil {
		t.Fatal(err)
	}
	host := &compactTestHost{context: agentcontext.NewManager(nil)}
	if err := host.AppendItems(context.Background(), "turn-1", user, assistant); err != nil {
		t.Fatal(err)
	}
	session := newTestSession(host.lines, host.context)
	session.state.Configuration = Configuration{Runtime: configured, CWD: root.Path(), AmadeusRoot: t.TempDir(), Mode: turn.ModeKindDefault}
	services, err := buildSessionServices(context.Background(), session, session.services, adapters)
	if err != nil {
		t.Fatal(err)
	}
	session.services = services
	t.Cleanup(func() { _ = session.services.Close() })
	return session, host, client
}

func compactRetryFailure(message string) llm.Stream {
	return &compactTestStream{chunks: nil, err: &llm.ProviderError{
		Kind: llm.ProviderErrorNetwork, Message: message, AdditionalDetails: message, Retryable: true, RetryDelay: time.Nanosecond,
	}}
}

func compactStreamErrorCounts(events []protocol.Event) (retrying, terminal int) {
	for _, event := range events {
		streamError, ok := event.Msg.(protocol.StreamErrorEvent)
		if !ok {
			continue
		}
		if streamError.WillRetry {
			retrying++
		} else {
			terminal++
		}
	}
	return retrying, terminal
}

func mustLoadModelMessages(t *testing.T) llm.ModelMessages {
	t.Helper()
	messages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	return messages
}
