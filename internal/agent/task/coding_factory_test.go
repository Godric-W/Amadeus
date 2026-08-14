package task

import (
	"context"
	"errors"
	"io"
	"reflect"
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

type factoryTestHost struct{ context *agentcontext.Manager }

func (*factoryTestHost) AppendItems(context.Context, turn.ID, ...rollout.Item) error { return nil }
func (*factoryTestHost) History() []rollout.Line                                     { return nil }
func (*factoryTestHost) Publish(context.Context, protocol.SessionEvent) error        { return nil }
func (*factoryTestHost) Request(context.Context, protocol.InteractiveRequest) (protocol.Op, error) {
	return nil, errors.New("unexpected interactive request")
}
func (*factoryTestHost) UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error) {
	return plan.Snapshot{}, nil
}
func (host *factoryTestHost) Context() *agentcontext.Manager { return host.context }

func TestCodingFactoryPreparesIndependentRegularTask(t *testing.T) {
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
	defer factory.Close()
	host := &factoryTestHost{context: agentcontext.NewManager(nil)}
	requestContext := turn.Context{
		ThreadID: "thread-1", TurnID: "turn-1", Provider: configured.DefaultProvider, Model: provider.Model,
		CWD: root.Path(), InitialPermissionMode: turn.PermissionModeDefault, ToolNames: []string{"placeholder"},
	}
	prepared, err := factory.Prepare(context.Background(), host, PrepareRequest{Kind: KindRegular, Input: "inspect project", Context: requestContext})
	if err != nil {
		t.Fatal(err)
	}
	regular, ok := prepared.Task.(*regularTask)
	if !ok || regular.agent == nil {
		t.Fatalf("prepared task = %#v", prepared.Task)
	}
	if prepared.Context.CurrentDate != "2026-08-14" || prepared.Context.Timezone != "Asia/Shanghai" {
		t.Fatalf("resolved environment snapshot = %#v", prepared.Context)
	}
	if len(prepared.Context.ToolNames) == 0 || reflect.DeepEqual(prepared.Context.ToolNames, requestContext.ToolNames) {
		t.Fatalf("tool snapshot was not derived from real registry: %v", prepared.Context.ToolNames)
	}
	if requestContext.ToolNames[0] != "placeholder" {
		t.Fatalf("Prepare mutated caller context: %#v", requestContext)
	}
	if regular.agent.BaseInstructions.Text != baseInstructions.Text {
		t.Fatalf("agent base instructions = %q", regular.agent.BaseInstructions.Text)
	}
	if err := prepared.Task.Abort(context.Background(), host, &prepared.Context); err != nil {
		t.Fatal(err)
	}
	if !closer.closed {
		t.Fatal("prepared task did not release audit resources")
	}
}
