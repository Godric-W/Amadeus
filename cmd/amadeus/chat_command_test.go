package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type chatCommandClient struct {
	model    llm.ModelInfo
	requests []llm.Request
}

func (client *chatCommandClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("complete is not used by chat command")
}

func (client *chatCommandClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	return &chatCommandStream{chunks: []llm.StreamChunk{
		{ID: "response_1", ContentDelta: "answer"},
		{ID: "response_1", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
	}}, nil
}

func (client *chatCommandClient) Model() llm.ModelInfo {
	return client.model
}

func (client *chatCommandClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type chatCommandStream struct {
	chunks []llm.StreamChunk
	next   int
}

type cancellableChatClient struct {
	model    llm.ModelInfo
	started  chan struct{}
	requests []llm.Request
}

func (client *cancellableChatClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("complete is not used by chat command")
}

func (client *cancellableChatClient) Stream(ctx context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	if len(client.requests) == 1 {
		return &cancellableChatStream{ctx: ctx, started: client.started}, nil
	}
	return &chatCommandStream{chunks: []llm.StreamChunk{
		{ID: "response_2", ContentDelta: "answer"},
		{ID: "response_2", FinishReason: llm.FinishReasonStop},
	}}, nil
}

func (client *cancellableChatClient) Model() llm.ModelInfo {
	return client.model
}

func (client *cancellableChatClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type cancellableChatStream struct {
	ctx     context.Context
	started chan struct{}
	once    bool
}

func (stream *cancellableChatStream) Recv() (llm.StreamChunk, error) {
	if !stream.once {
		stream.once = true
		close(stream.started)
	}
	<-stream.ctx.Done()
	return llm.StreamChunk{}, &llm.ProviderError{
		Kind:    llm.ProviderErrorCancelled,
		Message: "request cancelled",
		Cause:   stream.ctx.Err(),
	}
}

func (stream *cancellableChatStream) Close() error {
	return nil
}

func (stream *chatCommandStream) Recv() (llm.StreamChunk, error) {
	if stream.next >= len(stream.chunks) {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[stream.next]
	stream.next++
	return chunk, nil
}

func (stream *chatCommandStream) Close() error {
	return nil
}

func TestChatCommandLoadsConfigurationAndRunsLoop(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
providers:
  openai:
    api: responses
    api_key: test-secret
    base_url: https://provider.example.invalid/v1
    model: configured-model
`)
	client := &chatCommandClient{}
	var factoryProviderName string
	var factoryProvider config.ProviderConfig
	command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
		amadeusRoot: amadeusRoot,
		lookupEnv:   emptyEnvLookup,
		llmClientFactory: func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
			factoryProviderName = providerName
			factoryProvider = provider
			client.model = llm.ModelInfo{Provider: providerName, Name: provider.Model}
			return client, nil
		},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("hello\nagain\n/exit\nignored\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"chat", "--model", "cli-model"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute chat command: %v", err)
	}
	if factoryProviderName != "openai" || factoryProvider.Model != "cli-model" || factoryProvider.APIKey != "test-secret" {
		t.Fatalf("unexpected effective provider: name=%q config=%#v", factoryProviderName, factoryProvider)
	}
	if len(client.requests) != 2 {
		t.Fatalf("unexpected request count: %#v", client.requests)
	}
	firstRequest := client.requests[0]
	if firstRequest.Model != "cli-model" || len(firstRequest.Messages) != 1 || !reflect.DeepEqual(firstRequest.Messages[0], llm.UserMessage("hello")) {
		t.Fatalf("unexpected first chat request: %#v", firstRequest)
	}
	expectedHistory := []llm.Message{llm.UserMessage("hello"), llm.AssistantMessage("answer"), llm.UserMessage("again")}
	if len(client.requests[1].Messages) != len(expectedHistory) {
		t.Fatalf("unexpected second chat history: %#v", client.requests[1].Messages)
	}
	for index, message := range expectedHistory {
		if !reflect.DeepEqual(client.requests[1].Messages[index], message) {
			t.Fatalf("unexpected second chat message %d: got %#v, want %#v", index, client.requests[1].Messages[index], message)
		}
	}
	if stdout.String() != "answer\nanswer\n" {
		t.Fatalf("unexpected chat stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected chat stderr: %q", stderr.String())
	}
}

func TestChatCommandExitAndEOFAreSuccessful(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
providers:
  openai:
    api_key: test-secret
    model: test-model
`)
	client := &chatCommandClient{model: llm.ModelInfo{Provider: "openai", Name: "test-model"}}
	for name, input := range map[string]string{
		"exit": "/exit\n",
		"eof":  "",
	} {
		t.Run(name, func(t *testing.T) {
			client.requests = nil
			command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
				amadeusRoot: amadeusRoot,
				lookupEnv:   emptyEnvLookup,
				llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
					return client, nil
				},
			})
			var output bytes.Buffer
			command.SetIn(strings.NewReader(input))
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs([]string{"chat"})
			if err := command.Execute(); err != nil {
				t.Fatalf("execute chat command: %v", err)
			}
			if len(client.requests) != 0 || output.Len() != 0 {
				t.Fatalf("exit behavior produced work: requests=%#v output=%q", client.requests, output.String())
			}
		})
	}
}

func TestChatCommandCancelsCurrentTurnAndContinues(t *testing.T) {
	amadeusRoot := t.TempDir()
	writeCommandConfig(t, filepath.Join(amadeusRoot, "config.yaml"), `
providers:
  openai:
    api_key: test-secret
    model: test-model
`)
	client := &cancellableChatClient{
		model:   llm.ModelInfo{Provider: "openai", Name: "test-model"},
		started: make(chan struct{}),
	}
	var cancelCurrent context.CancelFunc
	command := newRootCommandWithRuntime(&configFlags{}, commandRuntime{
		amadeusRoot: amadeusRoot,
		lookupEnv:   emptyEnvLookup,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			return client, nil
		},
		turnContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(parent)
			cancelCurrent = cancel
			return ctx, cancel
		},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("first\nsecond\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"chat"})
	done := make(chan error, 1)
	go func() {
		done <- command.Execute()
	}()
	<-client.started
	cancelCurrent()
	if err := <-done; err != nil {
		t.Fatalf("cancelled turn terminated chat command: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("chat command did not continue: %#v", client.requests)
	}
	if len(client.requests[1].Messages) != 1 || !reflect.DeepEqual(client.requests[1].Messages[0], llm.UserMessage("second")) {
		t.Fatalf("cancelled turn polluted history: %#v", client.requests[1].Messages)
	}
	if stdout.String() != "answer\n" {
		t.Fatalf("unexpected stdout after cancellation: %q", stdout.String())
	}
	if stderr.String() != "error: request cancelled\n" {
		t.Fatalf("unexpected cancellation stderr: %q", stderr.String())
	}
}

var _ llm.Client = (*chatCommandClient)(nil)
var _ llm.Client = (*cancellableChatClient)(nil)
var _ llm.Stream = (*chatCommandStream)(nil)
var _ llm.Stream = (*cancellableChatStream)(nil)
