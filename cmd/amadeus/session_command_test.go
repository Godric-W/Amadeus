package main

import (
	"bytes"
	"context"
	"errors"
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
	command.SetArgs(nil)
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
	command.SetArgs([]string{"--continue"})
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
	messages, err := store.ListMessages(context.Background(), sessions[0].ID)
	if err != nil || len(messages) != 4 {
		t.Fatalf("unexpected durable messages: %#v err=%v", messages, err)
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

func TestInterruptedRunCreatesNewRunWithReplanEnvelope(t *testing.T) {
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
	messages, err := store.ListMessages(context.Background(), sessions[0].ID)
	if err != nil || len(messages) != 3 {
		t.Fatalf("cancelled user and completed continuation messages missing: %#v err=%v", messages, err)
	}
	if len(first.requests) != 1 || len(second.streamRequests) == 0 {
		t.Fatalf("unexpected provider requests: first=%d second=%d", len(first.requests), len(second.streamRequests))
	}
	foundInterrupted := false
	for _, message := range second.streamRequests[0].Messages {
		if strings.Contains(message.Content, "amadeus.interrupted_work.v1") {
			foundInterrupted = true
		}
	}
	if !foundInterrupted {
		t.Fatalf("continuation request missing interrupted-work envelope: %#v", second.streamRequests[0].Messages)
	}
	if _, err := store.PendingInterruptedRun(context.Background(), sessions[0].ID); !errors.Is(err, sessiondomain.ErrNotFound) {
		t.Fatalf("successful continuation left pending interruption: %v", err)
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
	started, err := coordinator.BeginTask(context.Background(), "stored task", sessiondomain.RunMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.FinishTask(context.Background(), started, sessiondomain.RunCompleted, "", "done", nil); err != nil {
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
	command.SetArgs([]string{"--resume"})
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
