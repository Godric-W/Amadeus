package compact

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/modelclient"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type compactTestClient struct {
	requests []llm.Request
	errors   []error
}

func (*compactTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *compactTestClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	if len(client.errors) > 0 {
		err := client.errors[0]
		client.errors = client.errors[1:]
		if err != nil {
			return nil, err
		}
	}
	usage := llm.TokenUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
	return &compactTestStream{chunks: []llm.StreamChunk{
		{ContentDelta: "Current progress and next steps."},
		{FinishReason: llm.FinishReasonStop, TokenUsage: &usage},
	}}, nil
}

func (*compactTestClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "compact-model", ContextWindow: 128_000, InputModalities: []llm.InputModality{llm.InputModalityText}}
}

func (*compactTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type compactTestStream struct{ chunks []llm.StreamChunk }

func (stream *compactTestStream) Recv() (llm.StreamChunk, error) {
	if len(stream.chunks) == 0 {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[0]
	stream.chunks = stream.chunks[1:]
	return chunk, nil
}

func (*compactTestStream) Close() error { return nil }

func TestServiceUsesNormalBaseAndSyntheticUserPrompt(t *testing.T) {
	client := &compactTestClient{}
	service, modelSession := newCompactTestService(t, client)
	request := compactRequest(modelSession)
	output, err := service.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	prompt := client.requests[0].Prompt
	if prompt.BaseInstructions.Text != "normal base instructions" || len(prompt.Tools) != 0 {
		t.Fatalf("compaction prompt shape = %#v", prompt)
	}
	last := prompt.Input[len(prompt.Input)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Content, "CONTEXT CHECKPOINT COMPACTION") {
		t.Fatalf("synthetic compaction input = %#v", last)
	}
	if len(output.ReplacementHistory) < 2 || output.ReplacementHistory[len(output.ReplacementHistory)-1].Role != llm.RoleUser || !strings.Contains(output.ReplacementHistory[len(output.ReplacementHistory)-1].Content, "Another language model started") {
		t.Fatalf("replacement history = %#v", output.ReplacementHistory)
	}
	if output.TokenUsage.TotalTokens != 110 {
		t.Fatalf("token usage = %#v", output.TokenUsage)
	}
}

func TestServiceTrimsOldestCompleteGroupOnContextOverflow(t *testing.T) {
	client := &compactTestClient{errors: []error{&llm.ProviderError{Kind: llm.ProviderErrorContextWindow, Message: "too large"}, nil}}
	service, modelSession := newCompactTestService(t, client)
	request := compactRequest(modelSession)
	request.Source.PromptItems = append([]llm.ResponseItem{llm.UserMessage("oldest")}, request.Source.PromptItems...)
	if _, err := service.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 || len(client.requests[1].Prompt.Input) >= len(client.requests[0].Prompt.Input) {
		t.Fatalf("context overflow attempts = %#v", client.requests)
	}
}

func TestServiceRetainsOnlyTypedRealUserMessages(t *testing.T) {
	client := &compactTestClient{}
	service, modelSession := newCompactTestService(t, client)
	request := compactRequest(modelSession)
	request.Source.CanonicalHistory = append(request.Source.CanonicalHistory, llm.UserMessage("internal subagent notification"))
	request.Source.PromptItems = append(request.Source.PromptItems, llm.UserMessage("internal subagent notification"))
	output, err := service.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range output.ReplacementHistory {
		if strings.Contains(item.Content, "internal subagent notification") {
			t.Fatalf("internal notification entered replacement: %#v", output.ReplacementHistory)
		}
	}
}

func newCompactTestService(t *testing.T, client *compactTestClient) (*Service, *modelclient.ModelClientSession) {
	t.Helper()
	assets, err := internalprompt.LoadCompactionAssets()
	if err != nil {
		t.Fatal(err)
	}
	modelSession, err := modelclient.NewModelClientSession(client, modelclient.ModelClientSessionConfig{StreamIdleTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{ModelInfo: client.Model(), Assets: assets}, modelSession
}

func compactRequest(modelSession *modelclient.ModelClientSession) Request {
	history := []llm.ResponseItem{llm.UserMessage("first objective"), llm.AssistantMessage("work completed"), llm.UserMessage("latest request")}
	return Request{
		Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn,
		Source: Source{HistoryVersion: 3, CoveredThroughSequence: 3, SourceHash: "hash", CanonicalHistory: history, UserMessages: []llm.ResponseItem{history[0], history[2]}, PromptItems: append([]llm.ResponseItem{llm.DeveloperMessage("project context")}, history...)},
		Prompt: llm.Prompt{BaseInstructions: llm.BaseInstructions{Text: "normal base instructions"}}, Model: llm.ModelInfo{Name: "compact-model"},
		ModelSession: modelSession, Metadata: llm.RequestMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1"}, Events: protocol.NewMemorySink(),
	}
}
