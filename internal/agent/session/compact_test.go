package session

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
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
	events  []protocol.SessionEvent
}

func (host *compactTestHost) AppendItems(_ context.Context, turnID turn.ID, items ...rollout.Item) error {
	for _, item := range items {
		host.lines = append(host.lines, rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(len(host.lines) + 1), Timestamp: time.Now().UTC(), ThreadID: "thread-1", TurnID: turnID, Item: item})
	}
	return host.context.Rebuild(host.lines)
}

func (host *compactTestHost) History() []rollout.Line {
	return append([]rollout.Line(nil), host.lines...)
}
func (host *compactTestHost) Publish(_ context.Context, event protocol.SessionEvent) error {
	host.events = append(host.events, event)
	return nil
}
func (*compactTestHost) Request(context.Context, protocol.InteractiveRequest) (protocol.Op, error) {
	return nil, errors.New("unexpected interactive request")
}
func (*compactTestHost) UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error) {
	return plan.Snapshot{}, nil
}
func (host *compactTestHost) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	return host.context.Snapshot(model, prompt)
}
func (host *compactTestHost) ContextUpdate(key agentcontext.UpdateKey) string {
	return host.context.Update(key)
}

func TestCompactTaskProducesSemanticReplacementHistory(t *testing.T) {
	builder, host, client := newCompactionTestRuntime(t)
	session := newTestSession(host.lines, host.context)
	runtime, err := builder.BuildServices(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.request.Prompt.Input) < 3 || !strings.Contains(client.request.Prompt.BaseInstructions.Text, "CONTEXT CHECKPOINT COMPACTION") {
		t.Fatalf("compaction request was incomplete: %#v", client.request.Prompt)
	}
	if len(result.Items) != 2 || result.Items[0].Kind != rollout.KindCompaction || result.Items[1].Kind != rollout.KindTokenUsage {
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

func TestCompactTaskPreservesLatestUserTurnOutsideReplacement(t *testing.T) {
	builder, host, _ := newCompactionTestRuntime(t)
	session := newTestSession(host.lines, host.context)
	runtime, err := builder.BuildServices(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "now run the tests"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-2", latest); err != nil {
		t.Fatal(err)
	}
	session.state.History = host.lines
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext(), nil)
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
	builder, host, client := newCompactionTestRuntime(t)
	session := newTestSession(host.lines, host.context)
	runtime, err := builder.BuildServices(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	client.err = errors.New("provider unavailable")
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext(), nil)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("failed compaction result=%#v err=%v", result, err)
	}
}

func TestCompactTaskRetriesResponseStreamAndKeepsCanonicalResult(t *testing.T) {
	builder, host, client := newCompactionTestRuntimeWithRetries(t, 1)
	client.streams = []llm.Stream{compactRetryFailure("connection reset"), successfulCompactStream()}
	session := newTestSession(host.lines, host.context)
	runtime, err := builder.BuildServices(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 || len(result.Items) != 2 || result.Items[0].Kind != rollout.KindCompaction {
		t.Fatalf("retry compaction requests=%d result=%#v", len(client.requests), result)
	}
	if retrying, terminal := compactStreamErrorCounts(host.events); retrying != 1 || terminal != 0 {
		t.Fatalf("retry compaction events retrying=%d terminal=%d events=%#v", retrying, terminal, host.events)
	}
}

func TestCompactTaskRetryExhaustionDoesNotChangeReplacementHistory(t *testing.T) {
	builder, host, client := newCompactionTestRuntimeWithRetries(t, 1)
	client.streams = []llm.Stream{compactRetryFailure("first failure"), compactRetryFailure("second failure")}
	session := newTestSession(host.lines, host.context)
	original := session.History()
	runtime, err := builder.BuildServices(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&compactTask{runtime: runtime, events: host}).Run(context.Background(), session, compactTurnContext(), nil)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("exhausted compaction result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(session.History(), original) {
		t.Fatalf("failed compaction changed history: before=%#v after=%#v", original, session.History())
	}
	if retrying, terminal := compactStreamErrorCounts(host.events); retrying != 1 || terminal != 1 {
		t.Fatalf("exhausted compaction events retrying=%d terminal=%d events=%#v", retrying, terminal, host.events)
	}
}

func TestCompactionSuccessEventOrderRemainsContextWarningTerminal(t *testing.T) {
	item, err := rollout.NewItem(rollout.KindCompaction, rollout.Compaction{
		Summary: "summary", ReplacementHistory: []rollout.ReplacementMessage{{Role: "assistant", Content: "summary"}},
		CoveredThroughSequence: 1, SourceHash: "hash", Provider: "mock", Model: "compact-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{threadID: "thread-1", ctx: context.Background(), events: make(chan protocol.SessionEvent, 3)}
	session.publishCompactionEvents("turn-1", []rollout.Item{item})
	session.publish(protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.TurnCompleted{}})

	first := <-session.events
	second := <-session.events
	third := <-session.events
	if _, ok := first.Message.(protocol.ContextCompacted); !ok {
		t.Fatalf("first event = %T", first.Message)
	}
	if warning, ok := second.Message.(protocol.Warning); !ok || warning.Message != compactionWarningMessage {
		t.Fatalf("second event = %#v", second.Message)
	}
	if _, ok := third.Message.(protocol.TurnCompleted); !ok {
		t.Fatalf("third event = %T", third.Message)
	}
}

func compactTurnContext() *turn.TurnContext {
	return &turn.TurnContext{ThreadID: "thread-1", TurnID: "turn-2"}
}

func newCompactionTestRuntime(t *testing.T) (*ServicesBuilder, *compactTestHost, *interactiveCompactionClient) {
	configured := config.Default()
	return newCompactionTestRuntimeWithRetries(t, configured.Providers[configured.DefaultProvider].StreamMaxRetries)
}

func newCompactionTestRuntimeWithRetries(t *testing.T, streamMaxRetries int) (*ServicesBuilder, *compactTestHost, *interactiveCompactionClient) {
	t.Helper()
	client := &interactiveCompactionClient{}
	configured := config.Default()
	provider := configured.Providers[configured.DefaultProvider]
	provider.APIKey = "test-key"
	provider.Model = "compact-model"
	provider.MaxOutputTokens = 1024
	provider.StreamMaxRetries = streamMaxRetries
	configured.DefaultProvider = "mock"
	configured.Providers = map[string]config.ProviderConfig{"mock": provider}
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewServicesBuilder(ServicesOptions{
		Config: configured, Project: root, AmadeusRoot: t.TempDir(), ModelMessages: mustLoadModelMessages(t),
		ClientFactory: func(string, config.ProviderConfig) (llm.Client, error) { return client, nil },
		AuditFactory: func() (audit.Sink, io.Closer, error) {
			return audit.NewMemorySink(), io.NopCloser(strings.NewReader("")), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = builder.Close() })
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
	return builder, host, client
}

func compactRetryFailure(message string) llm.Stream {
	return &compactTestStream{chunks: nil, err: &llm.ProviderError{
		Kind: llm.ProviderErrorNetwork, Message: message, AdditionalDetails: message, Retryable: true, RetryDelay: time.Nanosecond,
	}}
}

func compactStreamErrorCounts(events []protocol.SessionEvent) (retrying, terminal int) {
	for _, event := range events {
		streamError, ok := event.Message.(protocol.StreamError)
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
