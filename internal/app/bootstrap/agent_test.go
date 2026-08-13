package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type bootstrapClient struct {
	model llm.ModelInfo
}

func (client *bootstrapClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("bootstrap test client Complete is not implemented")
}

func (client *bootstrapClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("bootstrap test client Stream is not implemented")
}

func (client *bootstrapClient) Model() llm.ModelInfo {
	return client.model
}

func (client *bootstrapClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

func TestNewAgentBuildsDefaultComposition(t *testing.T) {
	configured := validBootstrapConfig()
	root := newBootstrapProjectRoot(t)
	sink := protocol.NewMemorySink()

	agent, err := NewAgent(configured, root, sink, allowBootstrapApproval{}, audit.NewMemorySink())
	if err != nil {
		t.Fatalf("build default Agent composition: %v", err)
	}
	if agent.ProviderName != configured.DefaultProvider {
		t.Fatalf("unexpected provider name: got %q, want %q", agent.ProviderName, configured.DefaultProvider)
	}
	if agent.Project.Path() != root.Path() {
		t.Fatalf("unexpected project root: got %q, want %q", agent.Project.Path(), root.Path())
	}
	if agent.Client == nil || agent.Events != sink || agent.Audit == nil || agent.Registry == nil || agent.ToolService == nil || agent.Iterator == nil || agent.Progress == nil || agent.Runner == nil {
		t.Fatalf("Agent composition is incomplete: %#v", agent)
	}
	if !strings.Contains(agent.BaseInstructions.Text, "You are Amadeus") {
		t.Fatalf("Agent composition contains an unexpected BaseInstructions: %#v", agent.BaseInstructions)
	}
	if agent.Client.Model().Provider != configured.DefaultProvider || agent.Client.Model().Name != "test-model" {
		t.Fatalf("unexpected composed client model: %#v", agent.Client.Model())
	}
	if agent.Registry.Len() != 8 {
		t.Fatalf("unexpected Agent registry size: got %d, want 8", agent.Registry.Len())
	}
	wantTools := []string{"edit", "execute_command", "glob", "grep", "read", "view_image", "write", "write_stdin"}
	if got := toolNames(agent.AvailableTools()); !reflect.DeepEqual(got, wantTools) {
		t.Fatalf("unexpected available tools: got %v, want %v", got, wantTools)
	}
	for _, entry := range agent.Registry.Snapshot() {
		switch entry.Spec.Name {
		case "apply_patch":
			t.Fatalf("legacy tool entered default registry: %q", entry.Spec.Name)
		}
	}
}

func TestNewAgentRegistersOnlyEnabledWebTools(t *testing.T) {
	configured := validBootstrapConfig()
	configured.Web.Fetch.Enabled = true
	configured.Web.Search.Enabled = true
	agent, err := NewAgentWithOptions(configured, newBootstrapProjectRoot(t), protocol.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink(), AgentOptions{
		ClientFactory: successfulBootstrapFactory,
		WebFetcher:    bootstrapWebFetcher{document: webfetch.Document{URL: "https://example.test", Text: "ok"}},
		WebSearch:     bootstrapWebSearch{results: []websearch.Result{{Title: "One", URL: "https://example.test", Snippet: "ok"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"edit", "execute_command", "glob", "grep", "read", "web_fetch", "web_search", "write", "write_stdin"}
	if got := toolNames(agent.AvailableTools()); !reflect.DeepEqual(got, want) {
		t.Fatalf("configured Web tools = %v, want %v", got, want)
	}
}

type bootstrapWebFetcher struct{ document webfetch.Document }

func (fetcher bootstrapWebFetcher) Fetch(context.Context, string) (webfetch.Document, error) {
	return fetcher.document, nil
}

type bootstrapWebSearch struct{ results []websearch.Result }

func (provider bootstrapWebSearch) Search(context.Context, string, int) ([]websearch.Result, error) {
	return append([]websearch.Result(nil), provider.results...), nil
}

func TestNewAgentWithOptionsUsesStructuredWriteTool(t *testing.T) {
	root := newBootstrapProjectRoot(t)
	agent, err := NewAgentWithOptions(validBootstrapConfig(), root, protocol.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink(), AgentOptions{ClientFactory: successfulBootstrapFactory})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ToolService.Execute(context.Background(), tool.NewCall("write-1", "write", json.RawMessage(`{"path":"hook.txt","content":"value\n"}`))); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(root.Path(), "hook.txt")); err != nil || string(content) != "value\n" {
		t.Fatalf("structured write did not complete: %q %v", content, err)
	}
}

func TestNewAgentUsesOneSelectedProviderClient(t *testing.T) {
	configured := validBootstrapConfig()
	configured.DefaultProvider = "compatible"
	provider := configured.Providers[config.DefaultProviderName]
	provider.API = config.APIChatCompletions
	provider.Dialect = config.DialectDeepSeek
	provider.BaseURL = "https://compatible.example.invalid/v1"
	provider.Model = "compatible-model"
	configured.Providers["compatible"] = provider

	client := &bootstrapClient{model: llm.ModelInfo{Provider: "compatible", Name: "compatible-model"}}
	var factoryProviderName string
	var factoryProvider config.ProviderConfig
	root := newBootstrapProjectRoot(t)
	agent, err := newAgent(configured, root, protocol.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink(), func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
		factoryProviderName = providerName
		factoryProvider = provider
		return client, nil
	})
	if err != nil {
		t.Fatalf("build Agent composition with injected client: %v", err)
	}
	if factoryProviderName != "compatible" || factoryProvider.Model != "compatible-model" || factoryProvider.Dialect != config.DialectDeepSeek {
		t.Fatalf("factory received unexpected provider: name=%q config=%#v", factoryProviderName, factoryProvider)
	}
	if agent.Client != client {
		t.Fatal("composition did not retain the selected Provider client")
	}
}

func TestAgentCompositionExecutesStructuredWorkspaceWriteWithoutOperationApproval(t *testing.T) {
	configured := validBootstrapConfig()
	root := newBootstrapProjectRoot(t)
	agent, err := newAgent(configured, root, protocol.NewMemorySink(), denyBootstrapApproval{}, audit.NewMemorySink(), successfulBootstrapFactory)
	if err != nil {
		t.Fatalf("build secured Agent composition: %v", err)
	}
	execution, err := agent.ToolService.Execute(context.Background(), tool.NewCall(
		"write-denied", "write", json.RawMessage(`{"path":"denied.txt","content":"must not exist\n"}`),
	))
	if err != nil || execution.Outcome.Status != tool.ToolCallDenied {
		t.Fatalf("unexpected structured write result: execution=%#v err=%v", execution, err)
	}
	if _, statErr := os.Stat(filepath.Join(root.Path(), "denied.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("denied write produced a filesystem side effect: %v", statErr)
	}
}

func TestAgentCompositionRequiresApprovalPort(t *testing.T) {
	_, err := newAgent(validBootstrapConfig(), newBootstrapProjectRoot(t), protocol.NewMemorySink(), nil, audit.NewMemorySink(), successfulBootstrapFactory)
	if err == nil || !strings.Contains(err.Error(), "approval port is nil") {
		t.Fatalf("unexpected nil approval port error: %v", err)
	}
}

func TestAgentCompositionRequiresAuditSink(t *testing.T) {
	_, err := newAgent(validBootstrapConfig(), newBootstrapProjectRoot(t), protocol.NewMemorySink(), allowBootstrapApproval{}, nil, successfulBootstrapFactory)
	if err == nil || !strings.Contains(err.Error(), "audit sink is nil") {
		t.Fatalf("unexpected nil audit sink error: %v", err)
	}
}

func TestAgentCompositionFailsClosedBeforeCommandWhenAuditFails(t *testing.T) {
	configured := validBootstrapConfig()
	root := newBootstrapProjectRoot(t)
	auditSink := audit.NewMemorySink()
	expected := errors.New("audit disk unavailable")
	auditSink.SetError(expected)
	agent, err := newAgent(configured, root, protocol.NewMemorySink(), allowBootstrapApproval{}, auditSink, successfulBootstrapFactory)
	if err != nil {
		t.Fatalf("build audited Agent composition: %v", err)
	}
	outcome, err := agent.ToolService.Execute(context.Background(), tool.NewCall(
		"command-audit-failed", "execute_command", json.RawMessage(`{"command":"printf 'must not exist' > unaudited.txt"}`),
	))
	if err != nil || outcome.Outcome.Status != tool.ToolCallFailed || outcome.Outcome.Error == nil || !strings.Contains(outcome.Outcome.Error.Message, expected.Error()) {
		t.Fatalf("unexpected audit write failure: outcome=%#v err=%v", outcome, err)
	}
	if _, statErr := os.Stat(filepath.Join(root.Path(), "unaudited.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("audit failure produced a filesystem side effect: %v", statErr)
	}
}

func TestAgentAvailableToolsReturnsIndependentCopies(t *testing.T) {
	configured := validBootstrapConfig()
	agent, err := NewAgent(configured, newBootstrapProjectRoot(t), protocol.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink())
	if err != nil {
		t.Fatalf("build Agent composition: %v", err)
	}

	first := agent.AvailableTools()
	first[0].Name = "mutated"
	first[0].InputSchema[0] = 'x'
	second := agent.AvailableTools()
	if second[0].Name == "mutated" || second[0].InputSchema[0] == 'x' {
		t.Fatal("available tool snapshots share mutable storage")
	}
}

func TestNewAgentRejectsInvalidInputs(t *testing.T) {
	configured := validBootstrapConfig()
	root := newBootstrapProjectRoot(t)
	sink := protocol.NewMemorySink()
	factoryErr := errors.New("factory failed")

	tests := []struct {
		name       string
		configured config.Config
		root       project.Root
		events     protocol.EventSink
		factory    ClientFactory
		contains   string
	}{
		{
			name: "invalid config", configured: func() config.Config {
				invalid := configured
				invalid.Agent.MaxParallelTools = 0
				return invalid
			}(), root: root, events: sink, factory: successfulBootstrapFactory,
			contains: "validate Agent configuration",
		},
		{name: "empty project root", configured: configured, events: sink, factory: successfulBootstrapFactory, contains: "project root is empty"},
		{name: "nil event sink", configured: configured, root: root, factory: successfulBootstrapFactory, contains: "event sink is nil"},
		{name: "nil client factory", configured: configured, root: root, events: sink, contains: "client factory is nil"},
		{
			name: "client factory error", configured: configured, root: root, events: sink,
			factory:  func(string, config.ProviderConfig) (llm.Client, error) { return nil, factoryErr },
			contains: "create Provider client: factory failed",
		},
		{
			name: "client factory returns nil", configured: configured, root: root, events: sink,
			factory:  func(string, config.ProviderConfig) (llm.Client, error) { return nil, nil },
			contains: "factory returned nil",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newAgent(test.configured, test.root, test.events, allowBootstrapApproval{}, audit.NewMemorySink(), test.factory)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected bootstrap error: got %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestNewAgentReportsProviderConstructionErrors(t *testing.T) {
	configured := config.Default()
	_, err := NewAgent(configured, newBootstrapProjectRoot(t), protocol.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink())
	if err == nil || !strings.Contains(err.Error(), "create Provider client: provider model is empty") {
		t.Fatalf("unexpected missing model error: %v", err)
	}
}

func validBootstrapConfig() config.Config {
	configured := config.Default()
	provider := configured.Providers[configured.DefaultProvider]
	provider.APIKey = "test-key"
	provider.Model = "test-model"
	configured.Providers[configured.DefaultProvider] = provider
	return configured
}

type allowBootstrapApproval struct{}

func (allowBootstrapApproval) Decide(context.Context, policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	return policy.ApprovalDecision{
		Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce,
		Source: policy.ApprovalSourceUser, Reason: "bootstrap test approval",
	}, nil
}

type denyBootstrapApproval struct{}

func (denyBootstrapApproval) Decide(context.Context, policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	return policy.ApprovalDecision{
		Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce,
		Source: policy.ApprovalSourceUser, Reason: "bootstrap test denial",
	}, nil
}

func newBootstrapProjectRoot(t *testing.T) project.Root {
	t.Helper()
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create bootstrap project root: %v", err)
	}
	return root
}

func successfulBootstrapFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return &bootstrapClient{model: llm.ModelInfo{Provider: providerName, Name: provider.Model}}, nil
}

func toolNames(specs []tool.ToolSpec) []string {
	names := make([]string, len(specs))
	for index, spec := range specs {
		names[index] = spec.Name
	}
	return names
}
