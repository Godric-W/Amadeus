package bootstrap

import (
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentreflect "github.com/Godric-W/Amadeus/internal/agent/reflect"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaiadapter "github.com/Godric-W/Amadeus/internal/llm/openai"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/Godric-W/Amadeus/prompts"
)

const defaultDirectAttempts = 2

type ClientFactory func(string, config.ProviderConfig) (llm.Client, error)

type AgentOptions struct {
	ClientFactory ClientFactory
}

type Agent struct {
	ProviderName     string
	Project          project.Root
	Client           llm.Client
	Events           event.Sink
	Audit            audit.Sink
	ContextBuilder   *agentcontext.Builder
	PromptRepository *internalprompt.Repository
	PromptAssembler  *internalprompt.Assembler
	AgentPrompt      internalprompt.Bundle
	RetryPrompt      internalprompt.Bundle
	ReflectPrompt    internalprompt.Bundle
	Registry         *tool.Registry
	Validator        *tool.ArgumentValidator
	Grants           *policy.GrantCache
	Authorizer       *policy.ToolAuthorizer
	ToolExecutor     *react.ToolExecutor
	Iterator         *react.Iterator
	Progress         *react.ProgressMonitor
	Runner           *react.Runner
	Verifier         *engine.DeterministicVerifier
	Reflector        *agentreflect.Reflector
	Engine           *engine.DirectEngine
	tools            []tool.Spec
}

func NewAgent(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink) (*Agent, error) {
	return NewAgentWithOptions(configured, root, events, approvals, auditSink, AgentOptions{})
}

func NewAgentWithOptions(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, options AgentOptions) (*Agent, error) {
	createClient := options.ClientFactory
	if createClient == nil {
		createClient = defaultClientFactory
	}
	return newAgent(configured, root, events, approvals, auditSink, createClient)
}

func (agent *Agent) AvailableTools() []tool.Spec {
	if agent == nil {
		return nil
	}
	tools := make([]tool.Spec, len(agent.tools))
	for index, spec := range agent.tools {
		tools[index] = spec.Clone()
	}
	return tools
}

func newAgent(configured config.Config, root project.Root, events event.Sink, approvals policy.ApprovalHandler, auditSink audit.Sink, createClient ClientFactory) (*Agent, error) {
	if err := config.Validate(configured); err != nil {
		return nil, fmt.Errorf("validate Agent configuration: %w", err)
	}
	if root.Path() == "" {
		return nil, errors.New("bootstrap Agent project root is empty")
	}
	if events == nil {
		return nil, errors.New("bootstrap Agent event sink is nil")
	}
	if approvals == nil {
		return nil, errors.New("bootstrap Agent approval handler is nil")
	}
	if auditSink == nil {
		return nil, errors.New("bootstrap Agent audit sink is nil")
	}
	if createClient == nil {
		return nil, errors.New("bootstrap Agent client factory is nil")
	}

	providerName := configured.DefaultProvider
	provider := configured.Providers[providerName]
	client, err := createClient(providerName, provider)
	if err != nil {
		return nil, fmt.Errorf("create Provider client: %w", err)
	}
	if client == nil {
		return nil, errors.New("create Provider client: factory returned nil")
	}
	promptRepository, err := internalprompt.NewBuiltinRepository()
	if err != nil {
		return nil, fmt.Errorf("create Prompt repository: %w", err)
	}
	promptAssembler, err := internalprompt.NewAssembler(promptRepository)
	if err != nil {
		return nil, fmt.Errorf("create Prompt assembler: %w", err)
	}
	agentPrompt, err := assemblePrompts(promptAssembler, prompts.AgentLayers())
	if err != nil {
		return nil, fmt.Errorf("assemble Agent Prompt: %w", err)
	}
	retryPrompt, err := assemblePrompts(promptAssembler, []prompts.ID{prompts.EngineRetry})
	if err != nil {
		return nil, fmt.Errorf("assemble retry Prompt: %w", err)
	}
	reflectionPrompt, err := assemblePrompts(promptAssembler, []prompts.ID{prompts.TaskReflection})
	if err != nil {
		return nil, fmt.Errorf("assemble reflection Prompt: %w", err)
	}

	registry, err := builtin.NewMVPRegistry(root, builtin.DefaultMVPOptions())
	if err != nil {
		return nil, fmt.Errorf("create MVP tool registry: %w", err)
	}
	validator := tool.NewArgumentValidator()
	grants := policy.NewGrantCache()
	authorizer, err := policy.NewToolAuthorizerWithOptions(root, approvals, policy.ToolAuthorizerOptions{Grants: grants, Audit: auditSink, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create tool authorizer: %w", err)
	}
	toolExecutor, err := react.NewToolExecutorWithOptions(registry, validator, react.ToolExecutorOptions{Authorizer: authorizer, Events: events})
	if err != nil {
		return nil, fmt.Errorf("create tool executor: %w", err)
	}
	iterator, err := react.NewIteratorWithOptions(client, events, react.IteratorOptions{SystemPrompt: agentPrompt.Content})
	if err != nil {
		return nil, fmt.Errorf("create model iterator: %w", err)
	}
	progress := react.DefaultProgressMonitor()
	runner, err := react.NewRunner(iterator, toolExecutor, progress, react.RunnerOptions{
		Temperature:      provider.Temperature,
		MaxOutputTokens:  provider.MaxOutputTokens,
		MaxParallelTools: configured.Agent.MaxParallelTools,
	})
	if err != nil {
		return nil, fmt.Errorf("create ReAct runner: %w", err)
	}
	verifier := engine.NewDeterministicVerifier()
	reflector, err := agentreflect.New(client, agentreflect.Options{
		Temperature:     0,
		MaxOutputTokens: provider.MaxOutputTokens,
		SystemPrompt:    reflectionPrompt.Content,
	})
	if err != nil {
		return nil, fmt.Errorf("create reflector: %w", err)
	}
	directEngine, err := engine.NewDirectEngine(runner, verifier, reflector, events, engine.DirectEngineOptions{
		MaxAttempts: defaultDirectAttempts,
		RetryPrompt: retryPrompt.Content,
	})
	if err != nil {
		return nil, fmt.Errorf("create DirectEngine: %w", err)
	}

	entries := registry.Snapshot()
	availableTools := make([]tool.Spec, 0, len(entries))
	for _, entry := range entries {
		availableTools = append(availableTools, entry.Spec.Clone())
	}

	return &Agent{
		ProviderName:     providerName,
		Project:          root,
		Client:           client,
		Events:           events,
		Audit:            auditSink,
		ContextBuilder:   agentcontext.NewBuilder(),
		PromptRepository: promptRepository,
		PromptAssembler:  promptAssembler,
		AgentPrompt:      agentPrompt,
		RetryPrompt:      retryPrompt,
		ReflectPrompt:    reflectionPrompt,
		Registry:         registry,
		Validator:        validator,
		Grants:           grants,
		Authorizer:       authorizer,
		ToolExecutor:     toolExecutor,
		Iterator:         iterator,
		Progress:         progress,
		Runner:           runner,
		Verifier:         verifier,
		Reflector:        reflector,
		Engine:           directEngine,
		tools:            availableTools,
	}, nil
}

func assemblePrompts(assembler *internalprompt.Assembler, ids []prompts.ID) (internalprompt.Bundle, error) {
	layers := make([]string, len(ids))
	for index, id := range ids {
		layers[index] = string(id)
	}
	return assembler.Assemble(internalprompt.AssembleInput{Layers: layers})
}

func defaultClientFactory(providerName string, provider config.ProviderConfig) (llm.Client, error) {
	return openaiadapter.NewAdapter(providerName, provider)
}
