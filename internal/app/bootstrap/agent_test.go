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

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/prompts"
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
	sink := event.NewMemorySink()

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
	if agent.Client == nil || agent.Events != sink || agent.Audit == nil || agent.ContextBuilder == nil || agent.PromptRepository == nil || agent.PromptAssembler == nil || agent.Registry == nil || agent.Validator == nil || agent.Grants == nil || agent.Authorizer == nil || agent.ToolExecutor == nil || agent.Iterator == nil || agent.Progress == nil || agent.Runner == nil || agent.Verifier == nil || agent.Reflector == nil || agent.Engine == nil {
		t.Fatalf("Agent composition is incomplete: %#v", agent)
	}
	if agent.AgentPrompt.Content != prompts.AgentSystem() || agent.RetryPrompt.Content != prompts.RetryProtocol() || agent.ReflectPrompt.Content != prompts.ReflectionProtocol() {
		t.Fatalf("Agent composition contains unexpected Prompt bundles: agent=%#v retry=%#v reflect=%#v", agent.AgentPrompt, agent.RetryPrompt, agent.ReflectPrompt)
	}
	if len(agent.AgentPrompt.Sources) != len(prompts.AgentLayers()) || len(agent.AgentPrompt.SHA256) != 64 || len(agent.RetryPrompt.SHA256) != 64 || len(agent.ReflectPrompt.SHA256) != 64 {
		t.Fatalf("Agent Prompt metadata is incomplete: agent=%#v retry=%#v reflect=%#v", agent.AgentPrompt, agent.RetryPrompt, agent.ReflectPrompt)
	}
	if agent.Client.Model().Provider != configured.DefaultProvider || agent.Client.Model().Name != "test-model" {
		t.Fatalf("unexpected composed client model: %#v", agent.Client.Model())
	}
	if agent.Registry.Len() != 7 {
		t.Fatalf("unexpected MVP registry size: got %d, want 7", agent.Registry.Len())
	}
	wantTools := []string{"apply_patch", "execute_command", "glob_files", "grep_code", "list_dir", "read_file", "write_file"}
	if got := toolNames(agent.AvailableTools()); !reflect.DeepEqual(got, wantTools) {
		t.Fatalf("unexpected available tools: got %v, want %v", got, wantTools)
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
	agent, err := newAgent(configured, root, event.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink(), func(providerName string, provider config.ProviderConfig) (llm.Client, error) {
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

func TestAgentCompositionDeniesToolBeforeSideEffect(t *testing.T) {
	configured := validBootstrapConfig()
	root := newBootstrapProjectRoot(t)
	agent, err := newAgent(configured, root, event.NewMemorySink(), denyBootstrapApproval{}, audit.NewMemorySink(), successfulBootstrapFactory)
	if err != nil {
		t.Fatalf("build secured Agent composition: %v", err)
	}
	execution, err := agent.ToolExecutor.Execute(context.Background(), tool.NewCall(
		"write-denied", "write_file", json.RawMessage(`{"path":"denied.txt","content":"must not exist","mode":"create"}`),
	))
	if !errors.Is(err, policy.ErrToolDenied) || execution.Evidence.Verified {
		t.Fatalf("unexpected denied write result: execution=%#v err=%v", execution, err)
	}
	if _, statErr := os.Stat(filepath.Join(root.Path(), "denied.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("denied write produced a filesystem side effect: %v", statErr)
	}
}

func TestAgentCompositionRequiresApprovalHandler(t *testing.T) {
	_, err := newAgent(validBootstrapConfig(), newBootstrapProjectRoot(t), event.NewMemorySink(), nil, audit.NewMemorySink(), successfulBootstrapFactory)
	if err == nil || !strings.Contains(err.Error(), "approval handler is nil") {
		t.Fatalf("unexpected nil approval handler error: %v", err)
	}
}

func TestAgentCompositionRequiresAuditSink(t *testing.T) {
	_, err := newAgent(validBootstrapConfig(), newBootstrapProjectRoot(t), event.NewMemorySink(), allowBootstrapApproval{}, nil, successfulBootstrapFactory)
	if err == nil || !strings.Contains(err.Error(), "audit sink is nil") {
		t.Fatalf("unexpected nil audit sink error: %v", err)
	}
}

func TestAgentCompositionFailsClosedBeforeWriteWhenAuditFails(t *testing.T) {
	configured := validBootstrapConfig()
	root := newBootstrapProjectRoot(t)
	auditSink := audit.NewMemorySink()
	expected := errors.New("audit disk unavailable")
	auditSink.SetError(expected)
	agent, err := newAgent(configured, root, event.NewMemorySink(), allowBootstrapApproval{}, auditSink, successfulBootstrapFactory)
	if err != nil {
		t.Fatalf("build audited Agent composition: %v", err)
	}
	_, err = agent.ToolExecutor.Execute(context.Background(), tool.NewCall(
		"write-audit-failed", "write_file", json.RawMessage(`{"path":"unaudited.txt","content":"must not exist","mode":"create"}`),
	))
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected audit write failure: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root.Path(), "unaudited.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("audit failure produced a filesystem side effect: %v", statErr)
	}
}

func TestAgentAvailableToolsReturnsIndependentCopies(t *testing.T) {
	configured := validBootstrapConfig()
	agent, err := NewAgent(configured, newBootstrapProjectRoot(t), event.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink())
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
	sink := event.NewMemorySink()
	factoryErr := errors.New("factory failed")

	tests := []struct {
		name       string
		configured config.Config
		root       project.Root
		events     event.Sink
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
	_, err := NewAgent(configured, newBootstrapProjectRoot(t), event.NewMemorySink(), allowBootstrapApproval{}, audit.NewMemorySink())
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

func toolNames(specs []tool.Spec) []string {
	names := make([]string, len(specs))
	for index, spec := range specs {
		names[index] = spec.Name
	}
	return names
}
