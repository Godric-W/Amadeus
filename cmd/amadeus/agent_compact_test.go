package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
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

func TestCompactInteractiveSessionPersistsSemanticReplacementHistory(t *testing.T) {
	runner, runtime, configured, client := newCompactionTestRuntime(t)
	before := len(runtime.History().Items)
	message, err := runner.compactInteractiveSession(context.Background(), configured, runtime)
	if err != nil || message != "Conversation compacted" {
		t.Fatalf("compact Session: message=%q err=%v", message, err)
	}
	if len(client.request.Messages) < 3 || !strings.Contains(client.request.Messages[0].Content, "handoff summary") {
		t.Fatalf("compaction request was incomplete: %#v", client.request.Messages)
	}
	items := runtime.History().Items
	if len(items) != before+1 || items[len(items)-1].Kind != sessiondomain.RolloutContextCompaction || items[len(items)-1].RunID != "" {
		t.Fatalf("compaction rollout was not appended standalone: %#v", items)
	}
	payload, err := sessiondomain.DecodeContextCompaction(items[len(items)-1])
	if err != nil || payload.Provider != "mock" || payload.Model != "compact-model" || len(payload.ReplacementHistory) != 1 {
		t.Fatalf("decode compaction: payload=%#v err=%v", payload, err)
	}
	projection, err := sessiondomain.ProjectMessages(items)
	if err != nil || len(projection.Messages) != 1 || !strings.Contains(projection.Messages[0].Content, "Inspection completed") {
		t.Fatalf("replacement history was not authoritative: projection=%#v err=%v", projection, err)
	}
}

func TestCompactInteractiveSessionFailurePreservesHistory(t *testing.T) {
	runner, runtime, configured, client := newCompactionTestRuntime(t)
	client.err = errors.New("provider unavailable")
	before := runtime.History()
	if _, err := runner.compactInteractiveSession(context.Background(), configured, runtime); err == nil {
		t.Fatal("compaction failure unexpectedly succeeded")
	}
	after := runtime.History()
	if len(after.Items) != len(before.Items) {
		t.Fatalf("failed compaction mutated history: before=%d after=%d", len(before.Items), len(after.Items))
	}
}

func newCompactionTestRuntime(t *testing.T) (*agentController, *sessiondomain.SessionRuntime, config.Config, *interactiveCompactionClient) {
	t.Helper()
	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	sequence := 0
	coordinator, err := sessiondomain.NewCoordinator(sessiondomain.NewMemoryStore(), t.TempDir(), "project", sessiondomain.CoordinatorOptions{
		IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sessiondomain.NewSessionRuntime(coordinator)
	if err != nil {
		t.Fatal(err)
	}
	started, err := runtime.BeginRun(context.Background(), "inspect project", sessiondomain.RunMetadata{Mode: sessiondomain.RunModeExecute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.FinishRun(context.Background(), started, sessiondomain.RunCompleted, "", "inspection completed", nil); err != nil {
		t.Fatal(err)
	}
	client := &interactiveCompactionClient{}
	runner := &agentController{runtime: commandRuntime{
		llmClientFactory:    func(string, config.ProviderConfig) (llm.Client, error) { return client, nil },
		now:                 func() time.Time { now = now.Add(time.Second); return now },
		persistentIDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
	}}
	configured := config.Config{DefaultProvider: "mock", Providers: map[string]config.ProviderConfig{
		"mock": {Model: "compact-model", MaxOutputTokens: 1024},
	}}
	return runner, runtime, configured, client
}
