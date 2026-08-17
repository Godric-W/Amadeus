package session

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
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

type builderTestClient struct{ model llm.ModelInfo }

func (*builderTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (*builderTestClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("unexpected Stream call")
}

func (client *builderTestClient) Model() llm.ModelInfo { return client.model }

func (*builderTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type builderTestCloser struct{ closed bool }

func (closer *builderTestCloser) Close() error {
	closer.closed = true
	return nil
}

type builderTestHost struct {
	context *agentcontext.Manager
	lines   []rollout.Line
	runtime *engine.Services
}

func (host *builderTestHost) AgentServices() *engine.Services { return host.runtime }

func (host *builderTestHost) AppendItems(_ context.Context, turnID turn.ID, items ...rollout.Item) error {
	for _, item := range items {
		host.lines = append(host.lines, rollout.Line{Sequence: uint64(len(host.lines) + 1), TurnID: turnID, Item: item})
	}
	return host.context.Rebuild(host.lines)
}
func (host *builderTestHost) History() []rollout.Line {
	return append([]rollout.Line(nil), host.lines...)
}
func (*builderTestHost) Publish(context.Context, protocol.SessionEvent) error { return nil }
func (*builderTestHost) Request(context.Context, protocol.InteractiveRequest) (protocol.Op, error) {
	return nil, errors.New("unexpected interactive request")
}
func (*builderTestHost) UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error) {
	return plan.Snapshot{}, nil
}
func (host *builderTestHost) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	return host.context.Snapshot(model, prompt)
}
func (host *builderTestHost) ContextUpdate(key agentcontext.UpdateKey) string {
	return host.context.Update(key)
}

func TestServicesBuilderReusesSessionServicesAcrossRegularTasks(t *testing.T) {
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
	closer := &builderTestCloser{}
	modelMessages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewServicesBuilder(ServicesOptions{
		Config: configured, Project: root, AmadeusRoot: t.TempDir(), ModelMessages: modelMessages,
		ClientFactory: func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
			return &builderTestClient{model: llm.ModelInfo{Provider: providerName, Name: provider.Model}}, nil
		},
		AuditFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), closer, nil },
		Clock:        func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, location) },
	})
	if err != nil {
		t.Fatal(err)
	}
	host := &builderTestHost{context: agentcontext.NewManager(nil)}
	session := newTestSession(host.lines, host.context)
	runtime, err := builder.BuildServices(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	host.runtime = runtime
	requestContext := turn.TurnContext{
		ThreadID: "thread-1", TurnID: "turn-1", Provider: configured.DefaultProvider, Model: provider.Model,
		CWD: root.Path(), Mode: turn.ModeKindDefault,
	}
	session.services.AgentServices = runtime
	preparedTask, preparedContext, err := builder.NewRegularTask(context.Background(), session, "inspect project", requestContext)
	if err != nil {
		t.Fatal(err)
	}
	regular, ok := preparedTask.(*regularTask)
	if !ok || regular.runtime == nil {
		t.Fatalf("prepared task = %#v", preparedTask)
	}
	if preparedContext.CurrentDate != "2026-08-14" || preparedContext.Timezone != "Asia/Shanghai" {
		t.Fatalf("resolved environment snapshot = %#v", preparedContext)
	}
	secondContext := requestContext
	secondContext.TurnID = "turn-2"
	secondTask, _, err := builder.NewRegularTask(context.Background(), session, "inspect again", secondContext)
	if err != nil {
		t.Fatal(err)
	}
	secondRegular := secondTask.(*regularTask)
	if secondRegular.runtime != regular.runtime {
		t.Fatal("regular tasks did not reuse the session runtime")
	}
	if err := preparedTask.Abort(context.Background(), session, &preparedContext); err != nil {
		t.Fatal(err)
	}
	if closer.closed {
		t.Fatal("turn abort closed session-scoped audit resources")
	}
	if err := builder.Close(); err != nil {
		t.Fatal(err)
	}
	if !closer.closed {
		t.Fatal("services builder close did not release session runtime resources")
	}
}
