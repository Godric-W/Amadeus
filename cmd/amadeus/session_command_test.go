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

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func TestSessionsListShowsOnlyCurrentProject(t *testing.T) {
	home := t.TempDir()
	projectOne := t.TempDir()
	projectTwo := t.TempDir()
	store, err := defaultThreadStoreFactory(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstID, secondID := testutil.ThreadID(1), testutil.ThreadID(2)
	if _, err := store.Materialize(context.Background(), thread.CreateInput{SessionID: protocol.SessionIDFromThreadID(firstID), ID: firstID, CWD: projectOne, Title: "one", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(context.Background(), firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialize(context.Background(), thread.CreateInput{SessionID: protocol.SessionIDFromThreadID(secondID), ID: secondID, CWD: projectTwo, Title: "two", CreatedAt: now}); err != nil {
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
	if !strings.Contains(output.String(), firstID.String()) || strings.Contains(output.String(), secondID.String()) {
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
		llmClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return firstClient, nil },
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
	secondRuntime.llmClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return secondClient, nil }
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
