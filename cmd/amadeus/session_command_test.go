package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func TestInteractiveDraftCommandsDoNotCreateSession(t *testing.T) {
	home := t.TempDir()
	projectDirectory := t.TempDir()
	runtime := commandRuntime{amadeusRoot: home, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup, terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("/status\n/clear\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	store, err := defaultThreadStoreFactory(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	threads, err := store.ListThreads(context.Background(), state.ListQuery{CWD: projectDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 || !strings.Contains(stderr.String(), "session: draft") {
		t.Fatalf("draft commands persisted a thread: threads=%#v stderr=%s", threads, stderr.String())
	}
}

func TestContinueWithoutHistoryKeepsDraft(t *testing.T) {
	home := t.TempDir()
	projectDirectory := t.TempDir()
	runtime := commandRuntime{amadeusRoot: home, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup, terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain", "--continue"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "no previous session") {
		t.Fatalf("missing continue notice: %s", stderr.String())
	}
}

func TestSessionsListShowsOnlyCurrentProject(t *testing.T) {
	home := t.TempDir()
	projectOne := t.TempDir()
	projectTwo := t.TempDir()
	store, err := defaultThreadStoreFactory(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.Materialize(context.Background(), thread.CreateInput{ID: "thread-one", CWD: projectOne, Title: "one", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(context.Background(), "thread-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialize(context.Background(), thread.CreateInput{ID: "thread-two", CWD: projectTwo, Title: "two", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	runtime := commandRuntime{amadeusRoot: home, workingDirectory: projectOne, lookupEnv: emptyEnvLookup}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"sessions", "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "thread-one") || strings.Contains(output.String(), "thread-two") {
		t.Fatalf("unexpected sessions list: %s", output.String())
	}
}

func TestSessionPersistsAcrossCommandsAndContinueReplaysHistory(t *testing.T) {
	home := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, home)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("project readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstClient := &codingCommandClient{}
	firstRuntime := commandRuntime{
		amadeusRoot: home, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return false }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) { return firstClient, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
	}
	first := newRootCommandWithRuntime(&configFlags{}, firstRuntime)
	first.SetIn(strings.NewReader(""))
	first.SetOut(io.Discard)
	first.SetErr(io.Discard)
	first.SetArgs([]string{"first task"})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}
	secondClient := &codingCommandClient{}
	secondRuntime := firstRuntime
	secondRuntime.llmClientFactory = func(string, config.ProviderConfig) (llm.Client, error) { return secondClient, nil }
	second := newRootCommandWithRuntime(&configFlags{}, secondRuntime)
	second.SetIn(strings.NewReader(""))
	second.SetOut(io.Discard)
	second.SetErr(io.Discard)
	second.SetArgs([]string{"--continue", "second task"})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(secondClient.streamRequests) == 0 {
		t.Fatal("continued thread made no provider request")
	}
	contents := messageContents(secondClient.streamRequests[0].Prompt.Input)
	if !strings.Contains(contents, "first task") || !strings.Contains(contents, "second task") {
		t.Fatalf("continued request omitted canonical history: %s", contents)
	}
}

func TestResumeSelectorEscReturnsToDraftConversation(t *testing.T) {
	home := t.TempDir()
	projectDirectory := t.TempDir()
	store, err := defaultThreadStoreFactory(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialize(context.Background(), thread.CreateInput{ID: "thread-one", CWD: projectDirectory, Title: "one", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	runtime := commandRuntime{amadeusRoot: home, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup, terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("\n"))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain", "--resume"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "selection cancelled") {
		t.Fatalf("selector did not cancel: %s", stderr.String())
	}
}
