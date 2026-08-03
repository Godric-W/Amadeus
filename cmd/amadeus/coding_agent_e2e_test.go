package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/snapshot"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/web"
)

type codingWorkflowClient struct {
	completeIndex int
	streamIndex   int
	requests      []llm.Request
}

func (client *codingWorkflowClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.requests = append(client.requests, request)
	client.completeIndex++
	content := "PLAN\n- Fix Add and run the project tests"
	if client.completeIndex > 1 {
		content = "COMPLETE\nfixed Add and verified go test"
	}
	return llm.Response{Message: llm.AssistantMessage(content), FinishReason: llm.FinishReasonStop}, nil
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
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("inspect test config: %v", err)
	}
	writeE2EFile(t, filepath.Join(projectDirectory, "go.mod"), "module example.com/calc\n\ngo 1.26.0\n")
	writeE2EFile(t, filepath.Join(projectDirectory, "calc.go"), "package calc\n\nfunc Add(left, right int) int { return left - right }\n")
	writeE2EFile(t, filepath.Join(projectDirectory, "calc_test.go"), "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(1, 2) != 3 { t.Fatal(\"unexpected sum\") } }\n")

	client := &codingWorkflowClient{}
	auditSink := audit.NewMemorySink()
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
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
	command.SetIn(strings.NewReader("s\ns\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"/plan Fix Add and run the project tests"})
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
	for _, fragment := range []string{"tool started: read_file", "tool started: write_file", "tool started: execute_command", "result: completed"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("workflow stderr missing %q: %s", fragment, stderr.String())
		}
	}
	if len(auditSink.Snapshot()) != 3 || client.streamIndex != 4 || client.completeIndex != 2 {
		t.Fatalf("unexpected workflow trace: audit=%d streams=%d completions=%d", len(auditSink.Snapshot()), client.streamIndex, client.completeIndex)
	}
	content, err := os.ReadFile(filepath.Join(projectDirectory, ".amadeus", "snapshots", "coding-workflow-e2e", "changes.json"))
	if err != nil {
		t.Fatalf("read Run snapshot changes: %v", err)
	}
	var changes []snapshot.FileChange
	if err := json.Unmarshal(content, &changes); err != nil {
		t.Fatalf("decode Run snapshot changes: %v", err)
	}
	if want := []snapshot.FileChange{{Path: "calc.go", Kind: snapshot.ChangeModified}}; !reflect.DeepEqual(changes, want) {
		t.Fatalf("unexpected Run snapshot changes: got %#v, want %#v", changes, want)
	}
}

func writeE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write E2E fixture %s: %v", path, err)
	}
}

var _ llm.Client = (*codingWorkflowClient)(nil)

type skillWorkflowClient struct {
	completeIndex int
	streamIndex   int
	requests      []llm.Request
}

func (client *skillWorkflowClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.requests = append(client.requests, request)
	client.completeIndex++
	content := "PLAN\n- Load the review Skill and inspect its reference"
	if client.completeIndex > 1 {
		content = "COMPLETE\nreview Skill was loaded"
	}
	return llm.Response{Message: llm.AssistantMessage(content), FinishReason: llm.FinishReasonStop}, nil
}

func (client *skillWorkflowClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	client.streamIndex++
	switch client.streamIndex {
	case 1:
		return skillWorkflowToolStream("load-skill", "load_skill", `{"name":"review"}`), nil
	case 2:
		return skillWorkflowToolStream("read-reference", "read_skill_reference", `{"skill":"review","path":"guide.md"}`), nil
	case 3:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "skill-final", ContentDelta: "review Skill was loaded", Usage: &llm.Usage{InputTokens: 20, OutputTokens: 6, TotalTokens: 26}},
			{ID: "skill-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
		}}, nil
	default:
		return nil, fmt.Errorf("unexpected Skill workflow stream %d", client.streamIndex)
	}
}

func skillWorkflowToolStream(id, name, arguments string) llm.Stream {
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: id, ToolCalls: []llm.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(arguments)}}, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
		{ID: id, FinishReason: llm.FinishReasonToolCalls, ProviderFinishReason: "tool_calls"},
	}}
}

func (client *skillWorkflowClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "skill-workflow"}
}

func (client *skillWorkflowClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func TestCodingAgentSkillWorkflowUsesProjectOverrideAndNextRequestContext(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.MkdirAll(filepath.Join(amadeusHome, "skills", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDirectory, ".amadeus", "skills", "review", "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeE2EFile(t, filepath.Join(amadeusHome, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: User review guidance\n---\nUSER-SKILL-BODY\n")
	writeE2EFile(t, filepath.Join(projectDirectory, ".amadeus", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Project review guidance\n---\nPROJECT-SKILL-BODY\n")
	writeE2EFile(t, filepath.Join(projectDirectory, ".amadeus", "skills", "review", "references", "guide.md"), "project reference\n")

	client := &skillWorkflowClient{}
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) { return client, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		runIDFactory:     func() string { return "skill-workflow-e2e" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"/plan Review this project"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute Skill workflow: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "review Skill was loaded\n" || client.streamIndex != 3 || client.completeIndex != 2 {
		t.Fatalf("unexpected Skill workflow output: stdout=%q streams=%d completes=%d", stdout.String(), client.streamIndex, client.completeIndex)
	}
	if len(client.requests) != 5 {
		t.Fatalf("unexpected request count: %d", len(client.requests))
	}
	first, second := client.requests[1], client.requests[2]
	if !requestContains(first, "amadeus.skill_index.v1") || !requestContains(first, "Project review guidance") || requestContains(first, "PROJECT-SKILL-BODY") {
		t.Fatalf("initial request did not contain disclosure-safe project Skill index: %#v", first.Messages)
	}
	if !requestContains(second, "amadeus.skill_context.v1") || !requestContains(second, "PROJECT-SKILL-BODY") || requestContains(second, "USER-SKILL-BODY") {
		t.Fatalf("second request did not contain the project Skill body: %#v", second.Messages)
	}
	if !strings.Contains(stderr.String(), "tool started: load_skill") || !strings.Contains(stderr.String(), "tool started: read_skill_reference") {
		t.Fatalf("Skill tools did not execute: %s", stderr.String())
	}
}

func requestContains(request llm.Request, content string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, content) {
			return true
		}
	}
	return false
}

var _ llm.Client = (*skillWorkflowClient)(nil)

type integratedWorkflowClient struct{ complete, stream int }

func (client *integratedWorkflowClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	client.complete++
	content := "PLAN\n- Use the configured integrations"
	if client.complete > 1 {
		content = "COMPLETE\nintegrated workflow completed"
	}
	return llm.Response{Message: llm.AssistantMessage(content), FinishReason: llm.FinishReasonStop}, nil
}
func (client *integratedWorkflowClient) Stream(_ context.Context, _ llm.Request) (llm.Stream, error) {
	client.stream++
	calls := []struct{ id, name, arguments string }{
		{"load", "load_skill", `{"name":"review"}`},
		{"mcp-list", "mcp_list_tools", `{"server":"demo"}`},
		{"mcp", "mcp_call", `{"server":"demo","name":"echo","arguments":{"value":"hello"}}`},
		{"web", "web_fetch", `{"url":"https://example.com/article"}`},
		{"write", "write_file", `{"path":"report.txt","content":"integrated\n","mode":"create"}`},
	}
	if client.stream <= len(calls) {
		call := calls[client.stream-1]
		return skillWorkflowToolStream(call.id, call.name, call.arguments), nil
	}
	return &codingCommandStream{chunks: []llm.StreamChunk{{ID: "integrated-final", ContentDelta: "integrated workflow completed"}, {ID: "integrated-final", FinishReason: llm.FinishReasonStop}}}, nil
}
func (*integratedWorkflowClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "integrated"}
}
func (*integratedWorkflowClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

type integratedMCPClient struct {
	calls  int
	closed int
}

func (*integratedMCPClient) ListTools(context.Context) ([]mcp.RemoteTool, error) {
	return []mcp.RemoteTool{{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}, nil
}
func (client *integratedMCPClient) CallTool(_ context.Context, name string, arguments json.RawMessage) (mcp.RemoteResult, error) {
	client.calls++
	return mcp.RemoteResult{Text: name + ":" + string(arguments)}, nil
}
func (client *integratedMCPClient) Close() error { client.closed++; return nil }

type integratedWebFetcher struct{}

func (*integratedWebFetcher) Fetch(context.Context, string) (web.Document, error) {
	return web.Document{URL: "https://example.com/article", Title: "Article", Text: "web fixture"}, nil
}

type integratedWriteHook struct{ calls int }

func (hook *integratedWriteHook) After(_ context.Context, spec tool.Spec, _ tool.Call, _ tool.Result) ([]engine.Evidence, error) {
	if spec.SideEffect != tool.SideEffectWrite {
		return nil, nil
	}
	hook.calls++
	return []engine.Evidence{{ID: "diagnostic/write", Kind: engine.EvidenceDiagnostic, Source: "test", Summary: "diagnostic published", Verified: true}}, nil
}

func TestCodingWorkflowIntegratesSkillMCPWebSnapshotAndDiagnosticHook(t *testing.T) {
	amadeusHome, projectDirectory := t.TempDir(), t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	if err := os.MkdirAll(filepath.Join(amadeusHome, "skills", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDirectory, ".amadeus", "skills", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeE2EFile(t, filepath.Join(amadeusHome, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review\n---\nSkill body\n")
	writeE2EFile(t, filepath.Join(amadeusHome, "mcp.yaml"), "servers:\n  demo:\n    transport: stdio\n    command: fixture\n")
	client, remote, hook := &integratedWorkflowClient{}, &integratedMCPClient{}, &integratedWriteHook{}
	auditSink := audit.NewMemorySink()
	runtime := commandRuntime{amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup, terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, config.ProviderConfig) (llm.Client, error) { return client, nil }, mcpClientFactory: func(context.Context, mcp.ServerConfig) (mcp.Client, error) { return remote, nil }, webFetcher: &integratedWebFetcher{}, postWriteHooks: []react.PostExecutionHook{hook}, auditSinkFactory: func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil }, runIDFactory: func() string { return "integrated-m6" }}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader("s\ns\ns\ns\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"/plan Use all integrations"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run integrated workflow: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "integrated workflow completed\n" || client.stream != 6 || client.complete != 2 || remote.calls != 1 || remote.closed != 1 || hook.calls != 1 {
		t.Fatalf("unexpected integrated trace: stdout=%q client=%#v remote=%#v hook=%#v", stdout.String(), client, remote, hook)
	}
	if content, err := os.ReadFile(filepath.Join(projectDirectory, "report.txt")); err != nil || string(content) != "integrated\n" {
		t.Fatalf("write did not complete: content=%q err=%v", content, err)
	}
	if len(auditSink.Snapshot()) != 5 {
		t.Fatalf("network/write approval audit missing: %#v", auditSink.Snapshot())
	}
	changes, err := os.ReadFile(filepath.Join(projectDirectory, ".amadeus", "snapshots", "integrated-m6", "changes.json"))
	if err != nil || !strings.Contains(string(changes), "report.txt") {
		t.Fatalf("snapshot missing integrated write: %q err=%v", changes, err)
	}
}

var _ llm.Client = (*integratedWorkflowClient)(nil)
var _ mcp.Client = (*integratedMCPClient)(nil)
