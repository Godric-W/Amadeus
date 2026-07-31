package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type codingWorkflowClient struct {
	streamIndex int
	requests    []llm.Request
}

func (client *codingWorkflowClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.requests = append(client.requests, request)
	return llm.Response{Message: llm.AssistantMessage(`{"scope":"task","verdict":"accept"}`), FinishReason: llm.FinishReasonStop}, nil
}

func (client *codingWorkflowClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	client.streamIndex++
	var call llm.ToolCall
	switch client.streamIndex {
	case 1:
		call = llm.ToolCall{ID: "read-calc", Name: "read_file", Arguments: json.RawMessage(`{"path":"calc.go"}`)}
	case 2:
		call = llm.ToolCall{ID: "write-calc", Name: "write_file", Arguments: json.RawMessage(`{"path":"calc.go","content":"package calc\n\nfunc Add(left, right int) int { return left + right }\n","mode":"replace"}`)}
	case 3:
		call = llm.ToolCall{ID: "test-project", Name: "execute_command", Arguments: json.RawMessage(`{"command":"go test ./...","timeout_ms":30000}`)}
	default:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "workflow-final", ContentDelta: "fixed Add and verified go test", Usage: &llm.Usage{InputTokens: 20, OutputTokens: 6, TotalTokens: 26}},
			{ID: "workflow-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
		}}, nil
	}
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: "workflow-tool", ToolCalls: []llm.ToolCall{call}, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
		{ID: "workflow-tool", FinishReason: llm.FinishReasonToolCalls, ProviderFinishReason: "tool_calls"},
	}}, nil
}

func (client *codingWorkflowClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "coding-workflow"}
}

func (client *codingWorkflowClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func TestCodingAgentCommandReadsFixesTestsAndCompletes(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	configPath := filepath.Join(amadeusHome, "config.yaml")
	configured, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read test config: %v", err)
	}
	configured = []byte(strings.Replace(string(configured), "default: ask", "default: allow", 1))
	if err := os.WriteFile(configPath, configured, 0o600); err != nil {
		t.Fatalf("enable non-interactive approvals: %v", err)
	}
	writeE2EFile(t, filepath.Join(projectDirectory, "go.mod"), "module example.com/calc\n\ngo 1.26.0\n")
	writeE2EFile(t, filepath.Join(projectDirectory, "calc.go"), "package calc\n\nfunc Add(left, right int) int { return left - right }\n")
	writeE2EFile(t, filepath.Join(projectDirectory, "calc_test.go"), "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(1, 2) != 3 { t.Fatal(\"unexpected sum\") } }\n")

	client := &codingWorkflowClient{}
	auditSink := audit.NewMemorySink()
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return false }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) { return client, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil },
		runIDFactory:     func() string { return "coding-workflow-e2e" },
		agentContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"Fix Add and run the project tests"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute Coding Agent workflow: %v\nstderr=%s", err, stderr.String())
	}

	updated, err := os.ReadFile(filepath.Join(projectDirectory, "calc.go"))
	if err != nil || !strings.Contains(string(updated), "left + right") {
		t.Fatalf("Coding Agent did not fix calc.go: %q err=%v", updated, err)
	}
	if stdout.String() != "fixed Add and verified go test\n" {
		t.Fatalf("unexpected workflow stdout: %q", stdout.String())
	}
	for _, fragment := range []string{"tool: read_file", "tool: write_file", "tool: execute_command", "verification: passed", "reflection: accept", "result: completed"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("workflow stderr missing %q: %s", fragment, stderr.String())
		}
	}
	if len(auditSink.Snapshot()) != 3 || client.streamIndex != 4 {
		t.Fatalf("unexpected workflow trace: audit=%d streams=%d", len(auditSink.Snapshot()), client.streamIndex)
	}
}

func writeE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write E2E fixture %s: %v", path, err)
	}
}

var _ llm.Client = (*codingWorkflowClient)(nil)
