package session

import (
	"context"
	"errors"
	"io"
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
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type interactiveCompactionClient struct {
	request llm.Request
	err     error
}

func (client *interactiveCompactionClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.request = request
	if client.err != nil {
		return llm.Response{}, client.err
	}
	return llm.Response{Message: llm.AssistantMessage("## Handoff\n\nInspection completed; continue with tests."), FinishReason: llm.FinishReasonStop}, nil
}

func (*interactiveCompactionClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("unexpected stream call")
}

func (*interactiveCompactionClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "compact-model"}
}

func (*interactiveCompactionClient) Capabilities() llm.Capabilities { return llm.Capabilities{} }

type compactTestHost struct {
	lines   []rollout.Line
	context *agentcontext.Manager
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
func (*compactTestHost) Publish(context.Context, protocol.SessionEvent) error { return nil }
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
	result, err := (&compactTask{runtime: runtime}).Run(context.Background(), session, &turn.TurnContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.request.Prompt.Input) < 3 || !strings.Contains(client.request.Prompt.BaseInstructions.Text, "canonical history") {
		t.Fatalf("compaction request was incomplete: %#v", client.request.Prompt)
	}
	if len(result.Items) != 2 || result.Items[0].Kind != rollout.KindCompaction || result.Items[1].Kind != rollout.KindTokenUsage {
		t.Fatalf("compaction result = %#v", result)
	}
	if err := host.AppendItems(context.Background(), "turn-2", result.Items...); err != nil {
		t.Fatal(err)
	}
	projection, err := agentcontext.ProjectRolloutMessages(host.History())
	if err != nil || len(projection.Messages) != 2 || projection.Messages[0].Role != llm.RoleUser || !strings.Contains(projection.Messages[1].Content, "## Compaction Checkpoint") || !strings.Contains(projection.Messages[1].Content, "Inspection completed") {
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
	result, err := (&compactTask{runtime: runtime}).Run(context.Background(), session, &turn.TurnContext{}, nil)
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
	if len(projection.Messages) != 3 || projection.Messages[0].Content != "inspect project" || !strings.Contains(projection.Messages[1].Content, "## Compaction Checkpoint") || projection.Messages[2].Content != "now run the tests" {
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
	result, err := (&compactTask{runtime: runtime}).Run(context.Background(), session, &turn.TurnContext{}, nil)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("failed compaction result=%#v err=%v", result, err)
	}
}

func newCompactionTestRuntime(t *testing.T) (*ServicesBuilder, *compactTestHost, *interactiveCompactionClient) {
	t.Helper()
	client := &interactiveCompactionClient{}
	configured := config.Default()
	provider := configured.Providers[configured.DefaultProvider]
	provider.APIKey = "test-key"
	provider.Model = "compact-model"
	provider.MaxOutputTokens = 1024
	configured.DefaultProvider = "mock"
	configured.Providers = map[string]config.ProviderConfig{"mock": provider}
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewServicesBuilder(ServicesOptions{
		Config: configured, Project: root, AmadeusRoot: t.TempDir(), BaseInstructions: llm.BaseInstructions{Text: "test base instructions"},
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
