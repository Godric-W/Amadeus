package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func TestInteractiveDraftCommandsDoNotCreateSession(t *testing.T) {
	projectDirectory := t.TempDir()
	store := sessiondomain.NewMemoryStore()
	factoryCalls := 0
	runtime := commandRuntime{
		amadeusRoot:         t.TempDir(),
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) {
			factoryCalls++
			return store, nil, nil
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("/help\n/resume\n/exit\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute draft commands: %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("expected only /resume to open the store, got %d calls", factoryCalls)
	}
	if sessions, err := store.ListSessions(context.Background(), "missing"); err != nil || len(sessions) != 0 {
		t.Fatalf("draft commands created a session: sessions=%v err=%v", sessions, err)
	}
	if !strings.Contains(stderr.String(), "no previous sessions") {
		t.Fatalf("resume command did not remain a draft: %s", stderr.String())
	}
}

func TestContinueWithoutHistoryKeepsDraft(t *testing.T) {
	projectDirectory := t.TempDir()
	store := sessiondomain.NewMemoryStore()
	runtime := commandRuntime{
		amadeusRoot:         t.TempDir(),
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) {
			return store, nil, nil
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("/exit\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain", "--continue"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute --continue without history: %v", err)
	}
	if strings.Contains(stderr.String(), "continued") {
		t.Fatalf("empty --continue incorrectly resumed a session: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "no previous session") {
		t.Fatalf("missing empty --continue notice: %s", stderr.String())
	}
}

func TestSessionsListShowsOnlyCurrentProject(t *testing.T) {
	projectDirectory := t.TempDir()
	store := sessiondomain.NewMemoryStore()
	runtime := commandRuntime{
		amadeusRoot:      t.TempDir(),
		workingDirectory: projectDirectory,
		lookupEnv:        emptyEnvLookup,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) {
			return store, nil, nil
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"sessions", "list"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute sessions list: %v", err)
	}
	if output.String() != "No sessions for the current project.\n" {
		t.Fatalf("unexpected sessions list output: %q", output.String())
	}
}

func TestSessionPersistsAcrossRootCommandInstancesAndContinueReplaysHistory(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("session readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := sessiondomain.NewMemoryStore()
	makeRuntime := func(client *codingCommandClient, detector terminalDetector) commandRuntime {
		return commandRuntime{
			amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
			terminalDetector: detector, agentCommandFactory: defaultAgentCommandFactory,
			sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) { return store, nil, nil },
			llmClientFactory:    func(string, config.ProviderConfig) (llm.Client, error) { return client, nil },
			auditSinkFactory:    func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		}
	}
	firstClient := &codingCommandClient{}
	first := newRootCommandWithRuntime(&configFlags{}, makeRuntime(firstClient, func(io.Reader) bool { return false }))
	first.SetIn(strings.NewReader(""))
	first.SetOut(io.Discard)
	first.SetErr(io.Discard)
	first.SetArgs([]string{"first task"})
	if err := first.Execute(); err != nil {
		t.Fatalf("first root command: %v", err)
	}
	secondClient := &codingCommandClient{}
	second := newRootCommandWithRuntime(&configFlags{}, makeRuntime(secondClient, func(io.Reader) bool { return false }))
	second.SetIn(strings.NewReader(""))
	second.SetOut(io.Discard)
	second.SetErr(io.Discard)
	second.SetArgs([]string{"--continue", "second task"})
	if err := second.Execute(); err != nil {
		t.Fatalf("continued root command: %v", err)
	}
	project, err := store.GetProjectByCanonicalPath(context.Background(), projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(context.Background(), project.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("unexpected durable sessions: %#v err=%v", sessions, err)
	}
	items, err := store.ListItems(context.Background(), sessions[0].ID)
	if err != nil || len(items) != 8 {
		t.Fatalf("unexpected durable rollout: %#v err=%v", items, err)
	}
	if len(secondClient.streamRequests) == 0 {
		t.Fatal("continued run did not call the provider")
	}
	foundFirst := false
	for _, message := range secondClient.streamRequests[0].Messages {
		if message.Role == llm.RoleUser && message.Content == "first task" {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Fatalf("continued request did not replay first user task: %#v", secondClient.streamRequests[0].Messages)
	}
}

func TestResumeBuildsRequestFromCanonicalReplacementHistory(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	store := sessiondomain.NewMemoryStore()
	startedAt := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	started, err := store.BeginFirstRun(context.Background(), sessiondomain.BeginFirstRunInput{
		ProjectID: "project-compact", CanonicalPath: projectDirectory, ProjectName: filepath.Base(projectDirectory),
		SessionID: "session-compact", SessionTitle: "first task", RunID: "run-compact", UserItemID: "item-user",
		UserContent: "RAW USER HISTORY", Provider: "mock", Model: "mock", APIMode: "responses", Dialect: "openai",
		Mode: sessiondomain.RunModeExecute, StartedAt: startedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	assistantPayload, err := sessiondomain.EncodePayload(sessiondomain.AssistantMessagePayload{Content: "RAW ASSISTANT HISTORY"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(context.Background(), sessiondomain.FinishRunInput{
		SessionID: started.Session.ID, RunID: started.Run.ID, RunStatus: sessiondomain.RunCompleted,
		TerminalItems: []sessiondomain.AppendItem{{ID: "item-assistant", RunID: started.Run.ID, Kind: sessiondomain.RolloutAssistantMessage, Payload: assistantPayload, CreatedAt: startedAt.Add(time.Second)}},
		FinishedAt:    startedAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(context.Background(), started.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := sessiondomain.ProjectMessages(items)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projection.Messages)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	compactionPayload, err := sessiondomain.EncodePayload(sessiondomain.ContextCompactionPayload{
		Summary: "COMPACTED HISTORY", ReplacementHistory: []sessiondomain.CompactionHistoryItem{{Role: llm.RoleAssistant, Content: "COMPACTED HISTORY"}},
		CoveredThroughSequence: items[len(items)-1].Sequence, SourceHash: hex.EncodeToString(digest[:]), Provider: "mock", Model: "mock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItems(context.Background(), sessiondomain.AppendItemsInput{
		SessionID: started.Session.ID,
		Items:     []sessiondomain.AppendItem{{ID: "item-compaction", RunID: started.Run.ID, Kind: sessiondomain.RolloutContextCompaction, Payload: compactionPayload, CreatedAt: startedAt.Add(2 * time.Second)}},
	}); err != nil {
		t.Fatal(err)
	}

	client := &finalOnlyCodingClient{content: "resumed"}
	idSequence := 0
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return false }, agentCommandFactory: defaultAgentCommandFactory,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) { return store, nil, nil },
		llmClientFactory:    func(string, config.ProviderConfig) (llm.Client, error) { return client, nil },
		auditSinkFactory:    func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		persistentIDFactory: func(kind string) string { idSequence++; return fmt.Sprintf("new-%s-%d", kind, idSequence) },
		runIDFactory:        func() string { return "run-resumed" },
		now:                 func() time.Time { return startedAt.Add(3 * time.Second) },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--resume", string(started.Session.ID), "next task"})
	if err := command.Execute(); err != nil {
		t.Fatalf("resume compacted Session: %v\nstderr=%s", err, stderr.String())
	}
	if len(client.streamRequests) != 1 || !requestContains(client.streamRequests[0], "COMPACTED HISTORY") || requestContains(client.streamRequests[0], "RAW USER HISTORY") || requestContains(client.streamRequests[0], "RAW ASSISTANT HISTORY") {
		t.Fatalf("Resume did not use canonical Replacement History: %#v", client.streamRequests)
	}
}

type interruptThenCompleteClient struct {
	delegate *codingCommandClient
	cancel   context.CancelFunc
	first    bool
	requests []llm.Request
}

func (client *interruptThenCompleteClient) Stream(ctx context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	if client.first {
		client.first = false
		if client.cancel != nil {
			client.cancel()
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return client.delegate.Stream(ctx, request)
}

func (client *interruptThenCompleteClient) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	return client.delegate.Complete(ctx, request)
}

func (client *interruptThenCompleteClient) Model() llm.ModelInfo { return client.delegate.Model() }
func (client *interruptThenCompleteClient) Capabilities() llm.Capabilities {
	return client.delegate.Capabilities()
}

func TestInterruptedRunCreatesNewRunFromCanonicalHistory(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("replan readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := sessiondomain.NewMemoryStore()
	first := &interruptThenCompleteClient{delegate: &codingCommandClient{}, first: true}
	second := &codingCommandClient{}
	clientIndex := 0
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) { return store, nil, nil },
		agentContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(parent)
			if clientIndex == 0 {
				first.cancel = cancel
			}
			return ctx, cancel
		},
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			if clientIndex == 0 {
				clientIndex++
				return first, nil
			}
			return second, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("first task\n请继续\n/exit\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interrupted continuation: %v\nstderr=%s", err, stderr.String())
	}
	project, err := store.GetProjectByCanonicalPath(context.Background(), projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(context.Background(), project.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("unexpected sessions after interruption: %#v err=%v", sessions, err)
	}
	items, err := store.ListItems(context.Background(), sessions[0].ID)
	if err != nil || len(items) != 6 {
		t.Fatalf("canonical interrupted continuation missing: %#v err=%v", items, err)
	}
	wantKinds := []sessiondomain.RolloutKind{
		sessiondomain.RolloutUserMessage,
		sessiondomain.RolloutRunInterrupted,
		sessiondomain.RolloutUserMessage,
		sessiondomain.RolloutToolCall,
		sessiondomain.RolloutToolResult,
		sessiondomain.RolloutAssistantMessage,
	}
	for index, want := range wantKinds {
		if items[index].Kind != want {
			t.Fatalf("rollout item %d kind = %q, want %q: %#v", index, items[index].Kind, want, items)
		}
	}
	if len(first.requests) != 1 || len(second.streamRequests) == 0 {
		t.Fatalf("unexpected provider requests: first=%d second=%d", len(first.requests), len(second.streamRequests))
	}
	foundInterruptedHistory := false
	foundInterruptedMarker := false
	for _, message := range second.streamRequests[0].Messages {
		if message.Role == llm.RoleUser && message.Content == "first task" {
			foundInterruptedHistory = true
		}
		if message.Role == llm.RoleDeveloper && strings.Contains(message.Content, "Previous Run interrupted:") {
			foundInterruptedMarker = true
		}
	}
	if !foundInterruptedHistory {
		t.Fatalf("continuation request missing canonical prior history: %#v", second.streamRequests[0].Messages)
	}
	if !foundInterruptedMarker {
		t.Fatalf("continuation request missing canonical interruption marker: %#v", second.streamRequests[0].Messages)
	}
}

func TestNewTaskAfterInterruptionKeepsCanonicalHistory(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("new objective readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := sessiondomain.NewMemoryStore()
	first := &interruptThenCompleteClient{delegate: &codingCommandClient{}, first: true}
	second := &codingCommandClient{}
	clientIndex := 0
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) { return store, nil, nil },
		agentContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(parent)
			if clientIndex == 0 {
				first.cancel = cancel
			}
			return ctx, cancel
		},
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			if clientIndex == 0 {
				clientIndex++
				return first, nil
			}
			return second, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("unfinished old task\nsummarize README instead\n/exit\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute new task after interruption: %v\nstderr=%s", err, stderr.String())
	}
	if len(second.streamRequests) == 0 {
		t.Fatal("new task did not call the provider")
	}
	messages := second.streamRequests[0].Messages
	if len(messages) == 0 || messages[len(messages)-1].Role != llm.RoleUser || messages[len(messages)-1].Content != "summarize README instead" {
		t.Fatalf("new objective is not the final current user message: %#v", messages)
	}
	foundPreviousUser := false
	for _, message := range messages[:len(messages)-1] {
		if message.Role == llm.RoleUser && message.Content == "unfinished old task" {
			foundPreviousUser = true
		}
	}
	if !foundPreviousUser {
		t.Fatalf("interrupted user history was not replayed: %#v", messages)
	}
}

func TestContinueRecoversAbandonedRunningRun(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("recovery readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := sessiondomain.NewMemoryStore()
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	sequence := 0
	coordinator, err := sessiondomain.NewCoordinator(store, projectDirectory, "project", sessiondomain.CoordinatorOptions{
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
		IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("legacy-%s-%d", kind, sequence) },
	})
	if err != nil {
		t.Fatal(err)
	}
	abandoned, err := coordinator.BeginRun(context.Background(), "unfinished task", sessiondomain.RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		now:                 func() time.Time { now = now.Add(time.Second); return now },
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) { return store, nil, nil },
		llmClientFactory:    func(string, config.ProviderConfig) (llm.Client, error) { return &codingCommandClient{}, nil },
		auditSinkFactory:    func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("finish safely\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain", "--continue"})
	if err := command.Execute(); err != nil {
		t.Fatalf("continue abandoned Run: %v\nstderr=%s", err, stderr.String())
	}
	recovered, err := store.GetRun(context.Background(), abandoned.Records.Run.ID)
	items, itemsErr := store.ListItems(context.Background(), abandoned.Records.Session.ID)
	if err != nil || itemsErr != nil || recovered.Status != sessiondomain.RunInterrupted || len(items) < 2 || items[1].Kind != sessiondomain.RolloutRunInterrupted {
		t.Fatalf("abandoned Run was not recovered: %#v err=%v", recovered, err)
	}
	if !strings.Contains(stdout.String(), "Task completed successfully.") || !strings.Contains(stderr.String(), "result: completed") {
		t.Fatalf("continuation did not complete: stdout=%q stderr=%s", stdout.String(), stderr.String())
	}
}

func TestResumeSelectorEscReturnsToDraftConversation(t *testing.T) {
	projectDirectory := t.TempDir()
	store := sessiondomain.NewMemoryStore()
	now := time.Now().UTC()
	sequence := 0
	coordinator, err := sessiondomain.NewCoordinator(store, projectDirectory, "project", sessiondomain.CoordinatorOptions{
		Clock:     func() time.Time { now = now.Add(time.Second); return now },
		IDFactory: func(kind string) string { sequence++; return fmt.Sprintf("%s-%d", kind, sequence) },
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := coordinator.BeginRun(context.Background(), "stored task", sessiondomain.RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.FinishRun(context.Background(), started, sessiondomain.RunCompleted, "", "done", nil); err != nil {
		t.Fatal(err)
	}
	runtime := commandRuntime{
		amadeusRoot: amadeusHomeForTest(t), workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		sessionStoreFactory: func(context.Context, string) (sessiondomain.Store, io.Closer, error) { return store, nil, nil },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("\x1b\n/exit\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain", "--resume"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute resume selector: %v", err)
	}
	if !strings.Contains(stderr.String(), "selection cancelled") || !strings.Contains(stderr.String(), "session: closed") {
		t.Fatalf("Esc did not return to current draft: %s", stderr.String())
	}
}

func amadeusHomeForTest(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}
