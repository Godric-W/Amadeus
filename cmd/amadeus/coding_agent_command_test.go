package main

import (
	"bytes"
	"context"
	"encoding/json"
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
)

type codingCommandClient struct {
	streamRequests   []llm.Request
	completeRequests []llm.Request
}

type interruptingCodingClient struct {
	*codingCommandClient
	cancel context.CancelFunc
}

type inlineApprovalCodingClient struct {
	completeRequests int
	streamRequests   int
}

func (client *interruptingCodingClient) Stream(ctx context.Context, _ llm.Request) (llm.Stream, error) {
	client.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (client *inlineApprovalCodingClient) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	client.completeRequests++
	content := "PLAN\n- Create the requested file"
	if client.completeRequests > 1 {
		content = "COMPLETE\nfile created"
	}
	return llm.Response{Message: llm.AssistantMessage(content), FinishReason: llm.FinishReasonStop}, nil
}

func (client *inlineApprovalCodingClient) Stream(_ context.Context, _ llm.Request) (llm.Stream, error) {
	client.streamRequests++
	switch client.streamRequests {
	case 1:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "write-tool", ToolCalls: []llm.ToolCall{{ID: "write-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"approved.txt","content":"approved\n","mode":"create"}`)}}, FinishReason: llm.FinishReasonToolCalls},
		}}, nil
	case 2:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "write-final", ContentDelta: "created"},
			{ID: "write-final", FinishReason: llm.FinishReasonStop},
		}}, nil
	default:
		return nil, errors.New("unexpected inline approval stream request")
	}
}

func (client *inlineApprovalCodingClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "mock-model"}
}

func (client *inlineApprovalCodingClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func (client *codingCommandClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.completeRequests = append(client.completeRequests, request)
	content := "PLAN\n- Inspect README and finish"
	if len(client.completeRequests) > 1 {
		content = "COMPLETE\nTask completed successfully."
	}
	return llm.Response{
		Message:      llm.AssistantMessage(content),
		FinishReason: llm.FinishReasonStop,
	}, nil
}

func (client *codingCommandClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.streamRequests = append(client.streamRequests, request)
	switch len(client.streamRequests) {
	case 1:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "response-tools", ToolCalls: []llm.ToolCall{{ID: "read-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
				Usage: &llm.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
			{ID: "response-tools", FinishReason: llm.FinishReasonToolCalls, ProviderFinishReason: "tool_calls"},
		}}, nil
	case 2:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "response-final", ContentDelta: "Task completed successfully.", Usage: &llm.Usage{InputTokens: 20, OutputTokens: 3, TotalTokens: 23}},
			{ID: "response-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
		}}, nil
	default:
		return nil, errors.New("unexpected extra stream request")
	}
}

func (client *codingCommandClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "mock-model"}
}

func (client *codingCommandClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

type codingCommandStream struct {
	chunks []llm.StreamChunk
	next   int
}

func (stream *codingCommandStream) Recv() (llm.StreamChunk, error) {
	if stream.next >= len(stream.chunks) {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[stream.next]
	stream.next++
	return chunk, nil
}

func (stream *codingCommandStream) Close() error { return nil }

func TestRootCommandUsesInlineRendererForTerminalOneShot(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(amadeusHome, "AGENTS.md"), []byte("user instruction"), 0o600); err != nil {
		t.Fatalf("write user instructions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDirectory, "AGENTS.md"), []byte("project instruction"), 0o600); err != nil {
		t.Fatalf("write project instructions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("project readme\n"), 0o600); err != nil {
		t.Fatalf("write project README: %v", err)
	}

	client := &codingCommandClient{}
	auditSink := audit.NewMemorySink()
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
			if providerName != "openai" || provider.Model != "mock-model" || provider.APIKey != "test-api-key" {
				t.Fatalf("unexpected Provider selection: name=%q provider=%#v", providerName, provider)
			}
			return client, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil },
		runIDFactory:     func() string { return "run-test" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"Inspect README and finish"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute real Coding Agent composition: %v\nstderr=%s", err, stderr.String())
	}

	if stdout.String() != "Task completed successfully.\n" {
		t.Fatalf("unexpected Coding Agent stdout: %q", stdout.String())
	}
	for _, fragment := range []string{
		"tool started: read_file", "tool completed: read_file", "status: phase=idle", "result: completed",
	} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("Coding Agent stderr missing %q: %s", fragment, stderr.String())
		}
	}
	if len(client.streamRequests) != 2 || len(client.completeRequests) != 0 {
		t.Fatalf("unexpected Provider request counts: stream=%d complete=%d", len(client.streamRequests), len(client.completeRequests))
	}
	first := client.streamRequests[0]
	if len(first.Messages) != 3 || first.Messages[0].Role != llm.RoleSystem || first.Messages[1].Role != llm.RoleDeveloper || first.Messages[2].Content != "Inspect README and finish" || len(first.Tools) != 10 {
		t.Fatalf("unexpected first Agent request: %#v", first)
	}
	if !strings.Contains(first.Messages[1].Content, "user instruction") || !strings.Contains(first.Messages[1].Content, "project instruction") {
		t.Fatalf("instruction envelope is incomplete: %s", first.Messages[1].Content)
	}
	second := client.streamRequests[1]
	if len(second.Messages) != 5 || second.Messages[3].Role != llm.RoleAssistant || second.Messages[4].Role != llm.RoleTool || !strings.Contains(second.Messages[4].Content, "project readme") {
		t.Fatalf("tool result was not replayed into the second request: %#v", second.Messages)
	}
	records := auditSink.Snapshot()
	if len(records) != 1 || records[0].ToolName != "read_file" || records[0].Outcome != audit.OutcomeAllow || records[0].Source != "policy" {
		t.Fatalf("unexpected authorization audit: %#v", records)
	}
}

func TestRootCommandRunsIndependentInteractiveTasksUntilExit(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("project readme\n"), 0o600); err != nil {
		t.Fatalf("write project README: %v", err)
	}

	var clients []*codingCommandClient
	var runSequence int
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			client := &codingCommandClient{}
			clients = append(clients, client)
			return client, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory: func() string {
			runSequence++
			return fmt.Sprintf("interactive-%d", runSequence)
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("first task\n\nsecond task\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interactive Coding Agent: %v\nstderr=%s", err, stderr.String())
	}

	if stdout.String() != "Task completed successfully.\nTask completed successfully.\n" || len(clients) != 2 {
		t.Fatalf("interactive Runs were not independent: stdout=%q clients=%d", stdout.String(), len(clients))
	}
	if strings.Count(stderr.String(), "result: completed") != 2 || !strings.Contains(stderr.String(), "session: closed") {
		t.Fatalf("interactive lifecycle output is incomplete: %s", stderr.String())
	}
}

func TestPlainInteractiveRunUsesTerminalApprovalPrompt(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	client := &inlineApprovalCodingClient{}
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			return client, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory:     func() string { return "inline-approval" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("create a file\ns\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute inline approval session: %v\nstderr=%s", err, stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(projectDirectory, "approved.txt"))
	if err != nil || string(content) != "approved\n" {
		t.Fatalf("approved write missing: content=%q err=%v", content, err)
	}
	for _, fragment := range []string{"approval: requested for write_file", "Approval required", "tool: write_file", "approval: allow for write_file", "result: completed", "session: closed"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("inline approval transcript missing %q: %s", fragment, stderr.String())
		}
	}
	if client.completeRequests != 0 || client.streamRequests != 2 || !strings.Contains(stdout.String(), "created") {
		t.Fatalf("inline approval run did not complete: complete=%d stream=%d stdout=%q", client.completeRequests, client.streamRequests, stdout.String())
	}
}

func TestInteractivePaletteCommandsExposeStatusAndTools(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("/help\n/status\n/tools\n/clear\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute palette commands: %v\nstderr=%s", err, stderr.String())
	}
	for _, fragment := range []string{
		"commands: /help, /plan, /exit, /clear, /resume, /status, /tools",
		"status: project=" + projectDirectory + " session=draft",
		"write_file (write)",
		"\x1b[2J\x1b[H",
		"session: closed",
	} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("palette output missing %q: %s", fragment, stderr.String())
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("palette commands unexpectedly wrote transcript output: %q", stdout.String())
	}
}

func TestInteractiveDumbTerminalUsesPlainRenderer(t *testing.T) {
	t.Setenv("TERM", "dumb")
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("project readme\n"), 0o600); err != nil {
		t.Fatalf("write project README: %v", err)
	}
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			return &codingCommandClient{}, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory:     func() string { return "dumb-terminal" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("finish task\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute dumb-terminal interactive Agent: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "run: started dumb-terminal") || strings.Contains(stderr.String(), "status: phase=") {
		t.Fatalf("dumb terminal did not use PlainRenderer: %s", stderr.String())
	}
}

func TestInteractiveInterruptCancelsCurrentRunAndContinues(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("project readme\n"), 0o600); err != nil {
		t.Fatalf("write project README: %v", err)
	}

	var contextCount int
	var currentCancel context.CancelFunc
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			if contextCount == 1 {
				return &interruptingCodingClient{codingCommandClient: &codingCommandClient{}, cancel: currentCancel}, nil
			}
			return &codingCommandClient{}, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory:     func() string { return fmt.Sprintf("interrupt-%d", contextCount) },
		agentContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
			contextCount++
			runCtx, cancel := context.WithCancel(parent)
			currentCancel = cancel
			return runCtx, cancel
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader("cancel this\nthen finish\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interrupted interactive Agent: %v\nstderr=%s", err, stderr.String())
	}

	if contextCount != 2 || stdout.String() != "Task completed successfully.\n" {
		t.Fatalf("interactive interrupt did not preserve the session: contexts=%d stdout=%q", contextCount, stdout.String())
	}
	for _, fragment := range []string{"result: cancelled", "result: completed", "session: closed"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("interactive interrupt output missing %q: %s", fragment, stderr.String())
		}
	}
}

func TestOneShotInterruptReturnsCancelledExitCode(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	var currentCancel context.CancelFunc
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return false },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) {
			return &interruptingCodingClient{codingCommandClient: &codingCommandClient{}, cancel: currentCancel}, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory:     func() string { return "cancelled-once" },
		agentContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
			runCtx, cancel := context.WithCancel(parent)
			currentCancel = cancel
			return runCtx, cancel
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"cancel task"})
	err := command.Execute()
	if exitCode(err) != exitCodeCancelled || !errorAlreadyReported(err) || !strings.Contains(stderr.String(), "result: cancelled") {
		t.Fatalf("unexpected one-shot interrupt result: code=%d err=%v stderr=%s", exitCode(err), err, stderr.String())
	}
}

func TestResolveAuditPathUsesXDGThenHome(t *testing.T) {
	lookup := config.EnvLookup(func(name string) (string, bool) {
		if name == envXDGStateHome {
			return "/state", true
		}
		return "", false
	})
	path, err := resolveAuditPath(lookup, func() (string, error) { return "", errors.New("must not be called") })
	if err != nil || path != filepath.Join("/state", "amadeus", "audit", "audit.jsonl") {
		t.Fatalf("unexpected XDG audit path: path=%q err=%v", path, err)
	}
	path, err = resolveAuditPath(emptyEnvLookup, func() (string, error) { return "/home/test", nil })
	if err != nil || path != filepath.Join("/home/test", ".local", "state", "amadeus", "audit", "audit.jsonl") {
		t.Fatalf("unexpected home audit path: path=%q err=%v", path, err)
	}
	if _, err := resolveAuditPath(emptyEnvLookup, nil); err == nil {
		t.Fatal("nil home resolver did not fail")
	}
}

func TestConfiguredAgentBudgetMapsEveryRunLimit(t *testing.T) {
	budget := configuredAgentBudget(config.AgentConfig{
		MaxSteps:         7,
		MaxToolCalls:     11,
		MaxInputTokens:   13_000,
		MaxOutputTokens:  17_000,
		MaxDuration:      19 * time.Minute,
		MaxParallelTools: 3,
	})

	if budget.MaxSteps != 7 || budget.MaxToolCalls != 11 || budget.MaxInputTokens != 13_000 || budget.MaxOutputTokens != 17_000 || budget.MaxDuration != 19*time.Minute {
		t.Fatalf("unexpected configured Agent budget: %#v", budget)
	}
}

func writeCodingCommandConfig(t *testing.T, directory string) {
	t.Helper()
	content := `version: 1
default_provider: openai
providers:
  openai:
    api: responses
    dialect: openai
    api_key: test-api-key
    base_url: https://example.invalid/v1
    model: mock-model
    timeout: 5s
    max_retries: 0
    temperature: 0.1
    max_output_tokens: 512
agent:
  max_steps: 8
  max_tool_calls: 12
  max_input_tokens: 10000
  max_output_tokens: 2000
  max_duration: 1m
  max_parallel_tools: 2
logging:
  level: info
  trace_llm: false
`
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write Coding Agent config: %v", err)
	}
}

var _ llm.Client = (*codingCommandClient)(nil)
var _ llm.Stream = (*codingCommandStream)(nil)

type plannedCodingCommandClient struct {
	streamRequests   []llm.Request
	completeRequests []llm.Request
	plan             string
}

func (client *plannedCodingCommandClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.completeRequests = append(client.completeRequests, request)
	switch len(client.completeRequests) {
	case 1:
		return llm.Response{Message: llm.AssistantMessage(client.plan), FinishReason: llm.FinishReasonStop}, nil
	default:
		return llm.Response{Message: llm.AssistantMessage("COMPLETE\nplanned synthesis"), FinishReason: llm.FinishReasonStop}, nil
	}
}

func (client *plannedCodingCommandClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.streamRequests = append(client.streamRequests, request)
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: "planned-final", ContentDelta: "task result", Usage: &llm.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
		{ID: "planned-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
	}}, nil
}

func (client *plannedCodingCommandClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "mock-model"}
}

func (client *plannedCodingCommandClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func TestInteractivePlanCommandRunsPlanExecuteEngine(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	client := &plannedCodingCommandClient{plan: "PLAN\n- inspect repository"}
	runtime := commandRuntime{
		amadeusRoot:         amadeusHome,
		workingDirectory:    projectDirectory,
		lookupEnv:           emptyEnvLookup,
		terminalDetector:    func(io.Reader) bool { return true },
		agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
			if providerName != "openai" || provider.Model != "mock-model" {
				t.Fatalf("unexpected provider: name=%q config=%#v", providerName, provider)
			}
			return client, nil
		},
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory:     func() string { return "planned-cli-run" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader("/plan inspect repository\n/exit\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--plain"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute planned task: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "planned synthesis\n" {
		t.Fatalf("unexpected planned synthesis output: %q", stdout.String())
	}
	for _, fragment := range []string{"result: completed", "session: closed"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("planned CLI output missing %q: %s", fragment, stderr.String())
		}
	}
	if len(client.streamRequests) != 1 || len(client.completeRequests) != 2 {
		t.Fatalf("unexpected planned provider calls: streams=%d complete=%d", len(client.streamRequests), len(client.completeRequests))
	}
}
