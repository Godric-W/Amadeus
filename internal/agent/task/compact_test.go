package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
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
	lines []rollout.Line
}

func (host *compactTestHost) AppendItems(_ context.Context, turnID turn.ID, items ...rollout.Item) error {
	for _, item := range items {
		host.lines = append(host.lines, rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(len(host.lines) + 1), Timestamp: time.Now().UTC(), ThreadID: "thread-1", TurnID: turnID, Item: item})
	}
	return nil
}

func (host *compactTestHost) History() []rollout.Line {
	return append([]rollout.Line(nil), host.lines...)
}

func TestCompactTaskProducesSemanticReplacementHistory(t *testing.T) {
	factory, host, client := newCompactionTestRuntime(t)
	result, err := (&compactTask{factory: factory}).Run(context.Background(), host, &turn.Context{}, nil)
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
	factory, host, _ := newCompactionTestRuntime(t)
	latest, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "now run the tests"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AppendItems(context.Background(), "turn-2", latest); err != nil {
		t.Fatal(err)
	}
	result, err := (&compactTask{factory: factory}).Run(context.Background(), host, &turn.Context{}, nil)
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
	factory, host, client := newCompactionTestRuntime(t)
	client.err = errors.New("provider unavailable")
	result, err := (&compactTask{factory: factory}).Run(context.Background(), host, &turn.Context{}, nil)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("failed compaction result=%#v err=%v", result, err)
	}
}

func newCompactionTestRuntime(t *testing.T) (*CodingFactory, *compactTestHost, *interactiveCompactionClient) {
	t.Helper()
	client := &interactiveCompactionClient{}
	configured := config.Config{DefaultProvider: "mock", Providers: map[string]config.ProviderConfig{"mock": {Model: "compact-model", MaxOutputTokens: 1024}}}
	factory := &CodingFactory{configured: configured, clientFactory: func(string, config.ProviderConfig) (llm.Client, error) { return client, nil }}
	user, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect project"})
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "inspection completed"})
	if err != nil {
		t.Fatal(err)
	}
	host := &compactTestHost{}
	if err := host.AppendItems(context.Background(), "turn-1", user, assistant); err != nil {
		t.Fatal(err)
	}
	return factory, host, client
}
