package integration

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
	"github.com/Godric-W/Amadeus/internal/cli"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type codingCommandClient struct {
	streamRequests   []llm.Request
	completeRequests []llm.Request
}

type finalOnlyCodingClient struct {
	streamRequests   []llm.Request
	completeRequests []llm.Request
	content          string
}

type toolFailureRecoveryClient struct {
	streamRequests   []llm.Request
	completeRequests []llm.Request
}

func (client *finalOnlyCodingClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.completeRequests = append(client.completeRequests, request)
	return llm.Response{}, errors.New("planner must not run for default ReAct")
}

func (client *finalOnlyCodingClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.streamRequests = append(client.streamRequests, request)
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: "final-only", ContentDelta: client.content},
		{ID: "final-only", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
	}}, nil
}

func (client *finalOnlyCodingClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "mock-model"}
}

func (client *finalOnlyCodingClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func (client *toolFailureRecoveryClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.completeRequests = append(client.completeRequests, request)
	return llm.Response{}, errors.New("planner must not run for default ReAct")
}

func (client *toolFailureRecoveryClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.streamRequests = append(client.streamRequests, request)
	switch len(client.streamRequests) {
	case 1:
		return &codingCommandStream{chunks: []llm.StreamChunk{{
			ID: "unknown-tool", ToolCalls: []llm.ToolCall{{ID: "unknown-1", Name: "missing_tool", Arguments: json.RawMessage(`{}`)}}, FinishReason: llm.FinishReasonToolCalls,
		}}}, nil
	case 2:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "recovered-final", ContentDelta: "Recovered after tool failure."},
			{ID: "recovered-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
		}}, nil
	default:
		return nil, errors.New("unexpected extra recovery stream request")
	}
}

func (client *toolFailureRecoveryClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "mock-model"}
}

func (client *toolFailureRecoveryClient) Capabilities() llm.Capabilities {
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
			{ID: "response-tools", ToolCalls: []llm.ToolCall{{ID: "read-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
				TokenUsage: &llm.TokenUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
			{ID: "response-tools", FinishReason: llm.FinishReasonToolCalls, ProviderFinishReason: "tool_calls"},
		}}, nil
	case 2:
		return &codingCommandStream{chunks: []llm.StreamChunk{
			{ID: "response-final", ContentDelta: "Task completed successfully.", TokenUsage: &llm.TokenUsage{InputTokens: 20, OutputTokens: 3, TotalTokens: 23}},
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

func TestDefaultGreetingUsesTurnEngineWithoutPlanner(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	client := &finalOnlyCodingClient{content: "你好！"}
	options := testAgentRootOptions(amadeusHome, projectDirectory, false)
	options.Bootstrap.ClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil }
	options.Bootstrap.NextID = testNextID("greeting-run")
	command := cli.NewRootCommand(options)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"你好"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute greeting: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "你好！\n" || len(client.streamRequests) != 1 || len(client.completeRequests) != 0 {
		t.Fatalf("greeting used wrong execution path: stdout=%q streams=%d completes=%d", stdout.String(), len(client.streamRequests), len(client.completeRequests))
	}
}

func TestTurnEngineRecoversFromToolFailure(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	client := &toolFailureRecoveryClient{}
	options := testAgentRootOptions(amadeusHome, projectDirectory, false)
	options.Bootstrap.ClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil }
	options.Bootstrap.NextID = testNextID("tool-recovery-run")
	command := cli.NewRootCommand(options)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"recover from a tool failure"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute tool recovery: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "Recovered after tool failure.\n" || len(client.streamRequests) != 2 || len(client.completeRequests) != 0 {
		t.Fatalf("unexpected tool recovery path: stdout=%q streams=%d completes=%d", stdout.String(), len(client.streamRequests), len(client.completeRequests))
	}
	followUp := client.streamRequests[1]
	if len(followUp.Prompt.Input) < 2 || followUp.Prompt.Input[len(followUp.Prompt.Input)-1].Role != llm.RoleTool || !strings.Contains(followUp.Prompt.Input[len(followUp.Prompt.Input)-1].Content, "missing_tool") {
		t.Fatalf("tool failure was not returned to TurnEngine: %#v", followUp.Prompt.Input)
	}
}

func TestCodingAgentPublishesNonFatalSkillLoadWarnings(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	invalidSkill := filepath.Join(amadeusHome, "skills", "broken", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(invalidSkill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidSkill, []byte("missing frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &finalOnlyCodingClient{content: "done"}
	options := testAgentRootOptions(amadeusHome, projectDirectory, false)
	options.Bootstrap.ClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil }
	options.Bootstrap.NextID = testNextID("skill-warning-run")
	command := cli.NewRootCommand(options)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"inspect"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute with invalid Skill: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "done\n" || !strings.Contains(stderr.String(), "warning:") || !strings.Contains(stderr.String(), "broken") {
		t.Fatalf("Skill warning was not surfaced without blocking the Run: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCodingAgentInjectsExplicitSkillIntoFirstRequestContext(t *testing.T) {
	amadeusHome := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, amadeusHome)
	skillPath := filepath.Join(projectDirectory, ".amadeus", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: review\ndescription: Review code\n---\nPROJECT-EXPLICIT-SKILL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &finalOnlyCodingClient{content: "done"}
	options := testAgentRootOptions(amadeusHome, projectDirectory, false)
	options.Bootstrap.ClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil }
	options.Bootstrap.NextID = testNextID("explicit-skill-run")
	command := cli.NewRootCommand(options)
	var stdout, stderr bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"$review inspect the changes"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute explicit Skill task: %v\nstderr=%s", err, stderr.String())
	}
	if len(client.streamRequests) != 1 || !requestContains(client.streamRequests[0], "amadeus.skill_injection.v2") || !requestContains(client.streamRequests[0], "PROJECT-EXPLICIT-SKILL") {
		t.Fatalf("explicit Skill was not frozen into the first RequestContext: %#v", client.streamRequests)
	}
}

func TestInteractiveApplicationProviderWorkflowUsesCanonicalToolLifecycle(t *testing.T) {
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
	options := testAgentRootOptions(amadeusHome, projectDirectory, true)
	options.Bootstrap.ClientFactory = func(providerName, model string, provider config.ModelProviderInfo) (llm.Client, error) {
		if providerName != "openai" || model != "mock-model" || provider.APIKey != "test-api-key" {
			t.Fatalf("unexpected Model/Provider selection: name=%q model=%q provider=%#v", providerName, model, provider)
		}
		return client, nil
	}
	options.Bootstrap.AuditFactory = func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil }
	options.Bootstrap.NextID = testNextID("run-test")
	command := cli.NewRootCommand(options)
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
		"Running read", "Ran read", "project readme", "result: completed",
	} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatalf("Coding Agent stderr missing %q: %s", fragment, stderr.String())
		}
	}
	if len(client.streamRequests) != 2 || len(client.completeRequests) != 0 {
		t.Fatalf("unexpected Provider request counts: stream=%d complete=%d", len(client.streamRequests), len(client.completeRequests))
	}
	first := client.streamRequests[0]
	if !strings.Contains(first.Prompt.BaseInstructions.Text, "You are Amadeus") {
		t.Fatalf("base instructions missing Amadeus identity")
	}
	if len(first.Prompt.Input) < 4 {
		t.Fatalf("first Agent request has too few messages: %d", len(first.Prompt.Input))
	}
	if first.Prompt.Input[0].Role != llm.RoleDeveloper || first.Prompt.Input[len(first.Prompt.Input)-1].Role != llm.RoleUser {
		t.Fatalf("unexpected first Agent message roles: first=%q last=%q", first.Prompt.Input[0].Role, first.Prompt.Input[len(first.Prompt.Input)-1].Role)
	}
	if first.Prompt.Input[len(first.Prompt.Input)-1].Content != "Inspect README and finish" {
		t.Fatalf("unexpected first Agent user message: %q", first.Prompt.Input[len(first.Prompt.Input)-1].Content)
	}
	if len(first.Prompt.Tools) != 13 {
		t.Fatalf("unexpected first Agent tool count: %d", len(first.Prompt.Tools))
	}
	for _, name := range []string{"spawn_agent", "send_input", "wait_agent", "close_agent"} {
		if !promptHasTool(first.Prompt.Tools, name) {
			t.Fatalf("first Agent request omitted multi-agent tool %q", name)
		}
	}
	firstPrompt := messageContents(first.Prompt.Input)
	for _, fragment := range []string{"## Execute Mode", "<collaboration_mode>", "<environment_context>", "<permission_context>", "## `read`", "user instruction", "project instruction"} {
		if !strings.Contains(firstPrompt, fragment) {
			t.Fatalf("first Agent request omitted Prompt fragment %q: %s", fragment, firstPrompt)
		}
	}
	second := client.streamRequests[1]
	if len(second.Prompt.Input) < len(first.Prompt.Input)+2 || second.Prompt.Input[len(second.Prompt.Input)-2].Role != llm.RoleAssistant || second.Prompt.Input[len(second.Prompt.Input)-1].Role != llm.RoleTool || !strings.Contains(second.Prompt.Input[len(second.Prompt.Input)-1].Content, "project readme") {
		t.Fatalf("tool result was not replayed into the second request: %#v", second.Prompt.Input)
	}
	records := auditSink.Snapshot()
	if len(records) != 0 {
		t.Fatalf("read-only Tool unexpectedly entered command approval audit: %#v", records)
	}
}

func promptHasTool(tools []llm.ToolSpec, name string) bool {
	for _, candidate := range tools {
		if candidate.Name == name {
			return true
		}
	}
	return false
}

func messageContents(messages []llm.ResponseItem) string {
	parts := make([]string, len(messages))
	for index, message := range messages {
		parts[index] = message.Content
	}
	return strings.Join(parts, "\n")
}

func writeCodingCommandConfig(t *testing.T, directory string) {
	t.Helper()
	content := `model: mock-model
model_provider: openai
model_context_window: 8192
tool_output_token_limit: 10000
model_providers:
  openai:
    wire_api: responses
    dialect: openai
    api_key: test-api-key
    base_url: https://example.invalid/v1
    timeout: 5s
    request_max_retries: 0
agent:
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

type plannedCodingCommandClient struct{ streamRequests []llm.Request }

func (client *plannedCodingCommandClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	return llm.Response{}, fmt.Errorf("unexpected non-streaming Plan Mode request: %#v", request)
}

func (client *plannedCodingCommandClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.streamRequests = append(client.streamRequests, request)
	return &codingCommandStream{chunks: []llm.StreamChunk{
		{ID: "planned-final", ContentDelta: "1. Inspect repository\n2. Implement changes\n3. Run tests", TokenUsage: &llm.TokenUsage{InputTokens: 10, OutputTokens: 12, TotalTokens: 22}},
		{ID: "planned-final", FinishReason: llm.FinishReasonStop, ProviderFinishReason: "stop"},
	}}, nil
}

func (client *plannedCodingCommandClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "mock-model"}
}

func (client *plannedCodingCommandClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true, SupportsDeveloperRole: true}
}

func messagesContain(messages []llm.ResponseItem, fragment string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, fragment) {
			return true
		}
	}
	return false
}
