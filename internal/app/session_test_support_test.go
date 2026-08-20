package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
)

type appTestClient struct {
	mu        sync.Mutex
	lastInput string
}

func (*appTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *appTestClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	text := "token budget"
	if !strings.Contains(request.Prompt.BaseInstructions.Text, "CONTEXT CHECKPOINT COMPACTION") {
		input := ""
		for index := len(request.Prompt.Input) - 1; index >= 0; index-- {
			if request.Prompt.Input[index].Role == llm.RoleUser {
				input = request.Prompt.Input[index].Content
				break
			}
		}
		client.mu.Lock()
		client.lastInput = input
		client.mu.Unlock()
		text = "done: " + input
	}
	return &appTestStream{chunks: []llm.StreamChunk{{ContentDelta: text}, {FinishReason: llm.FinishReasonStop}}}, nil
}

func (*appTestClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 128_000}
}

func (*appTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type appTestStream struct{ chunks []llm.StreamChunk }

func (stream *appTestStream) Recv() (llm.StreamChunk, error) {
	if len(stream.chunks) == 0 {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[0]
	stream.chunks = stream.chunks[1:]
	return chunk, nil
}

func (*appTestStream) Close() error { return nil }

func appSessionAdapters(t *testing.T, client llm.Client) agentsession.ServiceAdapters {
	t.Helper()
	messages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	return agentsession.ServiceAdapters{
		ModelMessages: messages,
		ClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
		AuditFactory:  func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
	}
}

func appSessionConfiguration(t *testing.T, cwd string) agentsession.Configuration {
	t.Helper()
	configured := config.Default()
	configured.ModelProvider = "mock"
	configured.Model = "model"
	configured.ModelContextWindow = 128_000
	configured.ModelAutoCompactTokenLimit = 115_200
	configured.ToolOutputTokenLimit = 10_000
	configured.ModelProviders = map[string]config.ModelProviderInfo{
		"mock": {WireAPI: config.WireAPIResponses, Dialect: config.DialectStandard, APIKey: "test", BaseURL: "https://example.invalid/v1", Timeout: time.Second, StreamIdleTimeout: time.Minute},
	}
	return agentsession.Configuration{Runtime: configured, CWD: cwd, AmadeusRoot: t.TempDir(), Mode: turn.ModeKindDefault}
}
