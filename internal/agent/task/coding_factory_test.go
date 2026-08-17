package task

import (
	"context"
	"errors"
	"io"
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

type factoryTestClient struct{ model llm.ModelInfo }

func (*factoryTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (*factoryTestClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("unexpected Stream call")
}

func (client *factoryTestClient) Model() llm.ModelInfo { return client.model }

func (*factoryTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type factoryTestCloser struct{ closed bool }

func (closer *factoryTestCloser) Close() error {
	closer.closed = true
	return nil
}

type factoryTestHost struct {
	context *agentcontext.Manager
	lines   []rollout.Line
}

func (host *factoryTestHost) AppendItems(_ context.Context, turnID turn.ID, items ...rollout.Item) error {
	for _, item := range items {
		host.lines = append(host.lines, rollout.Line{Sequence: uint64(len(host.lines) + 1), TurnID: turnID, Item: item})
	}
	return host.context.Rebuild(host.lines)
}
func (host *factoryTestHost) History() []rollout.Line {
	return append([]rollout.Line(nil), host.lines...)
}
func (*factoryTestHost) Publish(context.Context, protocol.SessionEvent) error { return nil }
func (*factoryTestHost) Request(context.Context, protocol.InteractiveRequest) (protocol.Op, error) {
	return nil, errors.New("unexpected interactive request")
}
func (*factoryTestHost) UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error) {
	return plan.Snapshot{}, nil
}
func (host *factoryTestHost) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	return host.context.Snapshot(model, prompt)
}
func (host *factoryTestHost) ContextUpdate(key agentcontext.UpdateKey) string {
	return host.context.Update(key)
}

func TestCodingFactoryReusesSessionRuntimeAcrossRegularTasks(t *testing.T) {
	configured := config.Default()
	provider := configured.Providers[configured.DefaultProvider]
	provider.APIKey = "test-key"
	provider.Model = "test-model"
	configured.Providers[configured.DefaultProvider] = provider
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	closer := &factoryTestCloser{}
	baseInstructions := llm.BaseInstructions{Text: "factory-owned base instructions"}
	factory, err := NewCodingFactory(CodingFactoryOptions{
		Config: configured, Project: root, AmadeusRoot: t.TempDir(), BaseInstructions: baseInstructions,
		ClientFactory: func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
			return &factoryTestClient{model: llm.ModelInfo{Provider: providerName, Name: provider.Model}}, nil
		},
		AuditFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), closer, nil },
		Clock:        func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, location) },
	})
	if err != nil {
		t.Fatal(err)
	}
	host := &factoryTestHost{context: agentcontext.NewManager(nil)}
	requestContext := turn.Context{
		ThreadID: "thread-1", TurnID: "turn-1", Provider: configured.DefaultProvider, Model: provider.Model,
		CWD: root.Path(), InitialPermissionMode: turn.PermissionModeDefault,
	}
	prepared, err := factory.Prepare(context.Background(), host, PrepareRequest{Kind: KindRegular, Input: "inspect project", Context: requestContext})
	if err != nil {
		t.Fatal(err)
	}
	regular, ok := prepared.Task.(*regularTask)
	if !ok || regular.runtime == nil {
		t.Fatalf("prepared task = %#v", prepared.Task)
	}
	if prepared.Context.CurrentDate != "2026-08-14" || prepared.Context.Timezone != "Asia/Shanghai" {
		t.Fatalf("resolved environment snapshot = %#v", prepared.Context)
	}
	secondContext := requestContext
	secondContext.TurnID = "turn-2"
	second, err := factory.Prepare(context.Background(), host, PrepareRequest{Kind: KindRegular, Input: "inspect again", Context: secondContext})
	if err != nil {
		t.Fatal(err)
	}
	secondRegular := second.Task.(*regularTask)
	if secondRegular.runtime != regular.runtime {
		t.Fatal("regular tasks did not reuse the session runtime")
	}
	if err := prepared.Task.Abort(context.Background(), host, &prepared.Context); err != nil {
		t.Fatal(err)
	}
	if closer.closed {
		t.Fatal("turn abort closed session-scoped audit resources")
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	if !closer.closed {
		t.Fatal("factory close did not release session runtime resources")
	}
}
