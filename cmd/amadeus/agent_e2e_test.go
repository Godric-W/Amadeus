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

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/webfetch"
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
		call = llm.ToolCall{ID: "read-calc", Name: "read", Arguments: json.RawMessage(`{"path":"calc.go"}`)}
	case 2:
		call = llm.ToolCall{ID: "write-calc", Name: "edit", Arguments: json.RawMessage(`{"path":"calc.go","old_string":"func Add(left, right int) int { return left - right }","new_string":"func Add(left, right int) int { return left + right }"}`)}
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

type planGuidedWorkflowClient struct {
	requests []llm.Request
	streams  int
}

func (client *planGuidedWorkflowClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("plan-guided workflow should not use non-streaming completion")
}

func (client *planGuidedWorkflowClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.requests = append(client.requests, request)
	client.streams++
	if client.streams == 1 {
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "plan-guided-tool", ToolCalls: []llm.ToolCall{{
				ID: "plan-1", Name: "update_plan", Arguments: json.RawMessage(`{"explanation":"Coordinate the multi-step change","plan":[{"step":"Inspect implementation","status":"in_progress"},{"step":"Apply focused fix","status":"pending"},{"step":"Run verification","status":"pending"}]}`),
			}}},
			{ID: "plan-guided-tool", FinishReason: llm.FinishReasonToolCalls, ProviderFinishReason: "tool_calls"},
		}}, nil
	}
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: "plan-guided-final", ContentDelta: "plan-guided workflow complete"},
		{ID: "plan-guided-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
	}}, nil
}

func (*planGuidedWorkflowClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "plan-guided"}
}

func (*planGuidedWorkflowClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func TestCodingAgentExposesAndExecutesUpdatePlan(t *testing.T) {
	amadeusHome, projectDirectory := t.TempDir(), t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	client := &planGuidedWorkflowClient{}
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		turnIDFactory:    func() string { return "plan-guided-e2e" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"Inspect several components, make a focused change, and verify it"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute plan-guided workflow: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "plan-guided workflow complete\n" || client.streams != 2 || len(client.requests) != 2 {
		t.Fatalf("unexpected plan-guided trace: stdout=%q streams=%d requests=%d", stdout.String(), client.streams, len(client.requests))
	}
	if !requestHasTool(client.requests[0], "update_plan") {
		t.Fatalf("execute request did not expose update_plan: %#v", client.requests[0].Prompt.Tools)
	}
	if prompt := requestPromptText(client.requests[0]); !strings.Contains(prompt, "## `update_plan`") || !strings.Contains(prompt, "multiple files or components") {
		t.Fatalf("execute request omitted plan guidance: %s", prompt)
	}
	if output := stderr.String(); !strings.Contains(output, "Updated Plan") || !strings.Contains(output, "Inspect implementation") || strings.Contains(output, "Running update_plan") {
		t.Fatalf("update_plan was not rendered as a dedicated plan update: %q", output)
	}
	if !requestContainsToolOutput(client.requests[1], "plan-1") {
		t.Fatalf("follow-up request omitted update_plan result: %#v", client.requests[1].Prompt.Input)
	}
}

func requestHasTool(request llm.Request, name string) bool {
	for _, definition := range request.Prompt.Tools {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func requestPromptText(request llm.Request) string {
	var content []string
	for _, message := range request.Prompt.Input {
		content = append(content, message.Content)
	}
	return strings.Join(content, "\n")
}

func requestContainsToolOutput(request llm.Request, callID string) bool {
	for _, message := range request.Prompt.Input {
		if message.Role == llm.RoleTool && message.ToolCallID == callID && strings.TrimSpace(message.Content) != "" {
			return true
		}
	}
	return false
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
		llmClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil },
		turnIDFactory:    func() string { return "coding-workflow-e2e" },
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
	for _, fragment := range []string{"Exploring", "Read calc.go", "Updating calc.go", "Running go test ./...", "result: completed"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("workflow stderr missing %q: %s", fragment, stderr.String())
		}
	}
	if len(auditSink.Snapshot()) != 1 || client.streamIndex != 4 || client.completeIndex != 0 {
		t.Fatalf("unexpected workflow trace: audit=%d streams=%d completions=%d", len(auditSink.Snapshot()), client.streamIndex, client.completeIndex)
	}
}

func writeE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write E2E fixture %s: %v", path, err)
	}
}

var _ llm.Client = (*codingWorkflowClient)(nil)

type addDirWorkflowClient struct {
	target string
	stream int
}

func (client *addDirWorkflowClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("complete must not be used")
}

func (client *addDirWorkflowClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	client.stream++
	if client.stream == 1 {
		arguments, err := json.Marshal(map[string]string{"path": client.target, "content": "shared\n"})
		if err != nil {
			return nil, err
		}
		return skillWorkflowToolStream("add-shared", "write", string(arguments)), nil
	}
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: "add-dir-final", ContentDelta: "updated shared workspace"},
		{ID: "add-dir-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
	}}, nil
}

func (client *addDirWorkflowClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "add-dir-workflow"}
}

func (client *addDirWorkflowClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func TestCodingAgentAddDirAllowsPatchAcrossWorkspaceRoots(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	additionalDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	target := filepath.Join(additionalDirectory, "shared.txt")
	client := &addDirWorkflowClient{target: target}
	runtime := commandRuntime{
		amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
		terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		turnIDFactory:    func() string { return "add-dir-e2e" },
	}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader("s\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--add-dir", additionalDirectory, "update shared workspace"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute --add-dir workflow: %v\nstderr=%s", err, stderr.String())
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "shared\n" {
		t.Fatalf("approved additional-root Patch did not complete: content=%q err=%v", content, err)
	}
	if stdout.String() != "updated shared workspace\n" || client.stream != 2 {
		t.Fatalf("unexpected --add-dir workflow: stdout=%q streams=%d", stdout.String(), client.stream)
	}
}

var _ llm.Client = (*addDirWorkflowClient)(nil)

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
		return skillWorkflowToolStream("load-skill", "read_skill", `{"name":"review"}`), nil
	case 2:
		return skillWorkflowToolStream("read-reference", "read_skill", `{"name":"review","path":"guide.md"}`), nil
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
		llmClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
		auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		turnIDFactory:    func() string { return "skill-workflow-e2e" },
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
	if stdout.String() != "review Skill was loaded\n" || client.streamIndex != 3 || client.completeIndex != 0 {
		t.Fatalf("unexpected Skill workflow output: stdout=%q streams=%d completes=%d", stdout.String(), client.streamIndex, client.completeIndex)
	}
	if len(client.requests) != 3 {
		t.Fatalf("unexpected request count: %d", len(client.requests))
	}
	first, second := client.requests[0], client.requests[1]
	if !requestContains(first, "amadeus.skill_index.v2") || !requestContains(first, "Project review guidance") || requestContains(first, "PROJECT-SKILL-BODY") {
		t.Fatalf("initial request did not contain disclosure-safe project Skill index: %#v", first.Prompt.Input)
	}
	if !requestContains(second, "PROJECT-SKILL-BODY") || requestContains(second, "USER-SKILL-BODY") {
		t.Fatalf("second request did not contain the project Skill body: %#v", second.Prompt.Input)
	}
	if strings.Count(stderr.String(), "Running read_skill") != 2 {
		t.Fatalf("Skill tools did not execute: %s", stderr.String())
	}
}

func requestContains(request llm.Request, content string) bool {
	for _, message := range request.Prompt.Input {
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
		{"load", "read_skill", `{"name":"review"}`},
		{"mcp-list", "mcp_list_tools", `{"server":"demo"}`},
		{"mcp", "mcp_call", `{"server":"demo","name":"echo","arguments":{"value":"hello"}}`},
		{"web", "web_fetch", `{"url":"https://example.com/article"}`},
		{"write", "write", `{"path":"report.txt","content":"integrated\n"}`},
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
func (*integratedMCPClient) ListResources(context.Context) ([]mcp.RemoteResource, error) {
	return []mcp.RemoteResource{{URI: "fixture://review", Name: "Review fixture", MIMEType: "text/plain"}}, nil
}
func (*integratedMCPClient) ReadResource(context.Context, string) ([]mcp.RemoteResourceContent, error) {
	return []mcp.RemoteResourceContent{{URI: "fixture://review", MIMEType: "text/plain", Text: "resource fixture"}}, nil
}
func (client *integratedMCPClient) CallTool(_ context.Context, name string, arguments json.RawMessage) (mcp.RemoteResult, error) {
	client.calls++
	return mcp.RemoteResult{Text: name + ":" + string(arguments)}, nil
}
func (client *integratedMCPClient) Close() error { client.closed++; return nil }

type integratedWebFetcher struct{}

func (*integratedWebFetcher) Fetch(context.Context, string) (webfetch.Document, error) {
	return webfetch.Document{URL: "https://example.com/article", Title: "Article", Text: "web fixture"}, nil
}

func TestCodingWorkflowIntegratesSkillMCPWebAndDiagnosticHook(t *testing.T) {
	amadeusHome, projectDirectory := t.TempDir(), t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	configPath := filepath.Join(amadeusHome, "config.yaml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("web:\n  fetch:\n    enabled: true\n")...)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(amadeusHome, "skills", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDirectory, ".amadeus", "skills", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeE2EFile(t, filepath.Join(amadeusHome, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review\n---\nSkill body\n")
	writeE2EFile(t, filepath.Join(amadeusHome, "mcp.yaml"), "servers:\n  demo:\n    transport: stdio\n    command: fixture\n")
	client, remote := &integratedWorkflowClient{}, &integratedMCPClient{}
	auditSink := audit.NewMemorySink()
	runtime := commandRuntime{amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup, terminalDetector: func(io.Reader) bool { return true }, agentCommandFactory: defaultAgentCommandFactory,
		llmClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil }, mcpClientFactory: func(context.Context, mcp.ServerConfig) (mcp.Client, error) { return remote, nil }, webFetcher: &integratedWebFetcher{}, auditSinkFactory: func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil }, turnIDFactory: func() string { return "integrated-m6" }}
	command := newRootCommandWithRuntime(&configFlags{}, runtime)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader("s\ns\ns\ns\n"))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"Use all integrations"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run integrated workflow: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "integrated workflow completed\n" || client.stream != 6 || client.complete != 0 || remote.calls != 1 || remote.closed != 1 {
		t.Fatalf("unexpected integrated trace: stdout=%q client=%#v remote=%#v", stdout.String(), client, remote)
	}
	if content, err := os.ReadFile(filepath.Join(projectDirectory, "report.txt")); err != nil || string(content) != "integrated\n" {
		t.Fatalf("write did not complete: content=%q err=%v", content, err)
	}
	if len(auditSink.Snapshot()) != 0 {
		t.Fatalf("structured integrations unexpectedly entered command approval audit: %#v", auditSink.Snapshot())
	}
}

var _ llm.Client = (*integratedWorkflowClient)(nil)
var _ mcp.Client = (*integratedMCPClient)(nil)
