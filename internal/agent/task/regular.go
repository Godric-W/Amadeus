package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (factory *CodingFactory) prepareRegular(ctx context.Context, host Host, snapshot turn.Context, goal string) (Prepared, error) {
	runtimeHost, ok := host.(eventRequestHost)
	if !ok {
		return Prepared{}, errors.New("session task host does not expose event and request boundaries")
	}
	events, err := protocol.NewScopedSink(runtimeHost, snapshot.ThreadID, snapshot.TurnID)
	if err != nil {
		return Prepared{}, err
	}
	approvals, err := newSessionApprovalPort(runtimeHost)
	if err != nil {
		return Prepared{}, err
	}
	auditSink, auditCloser, err := factory.auditFactory()
	if err != nil {
		return Prepared{}, err
	}
	if auditSink == nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return Prepared{}, errors.New("coding task audit factory returned nil sink")
	}
	planHost, ok := host.(PlanHost)
	if !ok {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return Prepared{}, errors.New("turn host does not support session plans")
	}
	options := agentruntime.AgentOptions{
		ClientFactory:   factory.clientFactory,
		RolloutRecorder: &turnRolloutRecorder{host: host, turnID: snapshot.TurnID},
		PlanUpdater:     planHost,
		UserSkillRoot:   factory.amadeusRoot, UserMCPRoot: factory.amadeusRoot,
		MCPClientFactory: factory.mcpClientFactory,
		Skills:           factory.extensions.Skills(), SkillWarnings: factory.extensions.SkillWarnings(), MCP: factory.extensions.MCP(),
		WebFetcher: factory.webFetcher, WebSearch: factory.webSearch,
		FileSystemPolicy: factory.fileSystemPolicy, Permissions: factory.permissions,
		BaseInstructions: factory.baseInstructions,
	}
	agent, err := agentruntime.NewAgentWithOptions(factory.configured, factory.project, events, approvals, auditSink, options)
	if err != nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return Prepared{}, err
	}
	availableTools := agent.AvailableTools()
	if snapshot.InitialPermissionMode == turn.PermissionModePlan {
		availableTools = planModeTools(availableTools)
	}
	snapshot.ToolNames = promptToolNames(availableTools)
	if err := snapshot.Validate(); err != nil {
		_ = agent.Close()
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return Prepared{}, err
	}
	sessionTask := &regularTask{factory: factory, goal: goal, agent: agent, availableTools: availableTools, auditCloser: auditCloser}
	return Prepared{Task: sessionTask, Context: snapshot}, nil
}

func (sessionTask *regularTask) run(ctx context.Context, host Host, turnContext *turn.Context) (result Result, runErr error) {
	if sessionTask == nil || sessionTask.factory == nil || sessionTask.agent == nil {
		return Result{}, errors.New("regular task is nil")
	}
	defer func() { runErr = errors.Join(runErr, sessionTask.close()) }()
	ctx = tool.WithInvocationMetadata(ctx, tool.InvocationMetadata{
		SessionID: string(turnContext.ThreadID), TurnID: string(turnContext.TurnID), Source: tool.ToolCallSourceModel,
	})
	for _, warning := range sessionTask.agent.SkillWarnings {
		if warning != nil {
			if err := sessionTask.agent.Events.Publish(ctx, protocol.SessionEvent{Message: protocol.Warning{Message: warning.Error()}}); err != nil {
				return Result{}, err
			}
		}
	}
	if err := prepareTurnContext(ctx, contextPreparationOptions{
		Task: sessionTask.goal, TurnContext: turnContext, Host: host, Agent: sessionTask.agent,
		Extensions: sessionTask.factory.extensions, FileSystemPolicy: sessionTask.factory.fileSystemPolicy,
		Instructions: sessionTask.factory.instructions, ApprovalCount: sessionTask.factory.permissions.GrantCount,
	}); err != nil {
		return Result{}, err
	}
	contextHost, ok := host.(ContextHost)
	if !ok || contextHost.Context() == nil {
		return Result{}, errors.New("session task host does not expose ContextManager")
	}
	provider := sessionTask.factory.configured.Providers[sessionTask.factory.configured.DefaultProvider]
	modelInfo := normalizedModelInfo(sessionTask.agent.Client.Model(), provider)
	promptShape := llm.Prompt{
		BaseInstructions: sessionTask.agent.BaseInstructions,
		Tools:            promptToolDefinitions(sessionTask.availableTools),
		OutputSchema:     append(llm.OutputSchema(nil), turnContext.OutputSchema...),
	}
	autoCompact := func(compactCtx context.Context) error {
		if !contextHost.Context().NeedsCompaction(modelInfo, promptShape) {
			return nil
		}
		compactResult, compactErr := (&compactTask{factory: sessionTask.factory}).Run(compactCtx, host, turnContext, nil)
		if compactErr != nil {
			if strings.Contains(compactErr.Error(), "no earlier turn") || strings.Contains(compactErr.Error(), "no safely compactable") {
				return nil
			}
			return compactErr
		}
		if len(compactResult.Items) == 0 {
			return nil
		}
		return host.AppendItems(compactCtx, turnContext.TurnID, compactResult.Items...)
	}
	return sessionTask.executeReactor(ctx, sessionTask.agent, contextHost.Context(), autoCompact, sessionTask.availableTools, llm.OutputSchema(turnContext.OutputSchema), turnContext.TurnID)
}

func (sessionTask *regularTask) close() error {
	if sessionTask == nil {
		return nil
	}
	sessionTask.closeOnce.Do(func() {
		if sessionTask.agent != nil {
			sessionTask.closeErr = errors.Join(sessionTask.closeErr, sessionTask.agent.Close())
		}
		if sessionTask.auditCloser != nil {
			sessionTask.closeErr = errors.Join(sessionTask.closeErr, sessionTask.auditCloser.Close())
		}
	})
	return sessionTask.closeErr
}

func normalizedModelInfo(model llm.ModelInfo, provider config.ProviderConfig) llm.ModelInfo {
	model.ContextWindow = provider.ContextWindow
	model.MaxOutputTokens = provider.MaxOutputTokens
	model.AutoCompactTokenLimit = provider.AutoCompactTokenLimit
	model.ToolOutputMaxTokens = provider.ToolOutputMaxTokens
	return model.Normalized()
}

func promptToolDefinitions(specs []tool.ToolSpec) []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, len(specs))
	for index, spec := range specs {
		definitions[index] = llm.ToolDefinition{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
	}
	return definitions
}

type OutcomeError struct {
	StopReason react.StopReason
	Reason     string
}

func (err *OutcomeError) Error() string {
	if err == nil {
		return ""
	}
	parts := []string{"result: " + outcomeName(err.StopReason)}
	if err.StopReason != "" {
		parts = append(parts, "stop_reason="+string(err.StopReason))
	}
	if reason := strings.TrimSpace(err.Reason); reason != "" {
		parts = append(parts, "reason="+foldSummary(reason))
	}
	return strings.Join(parts, " ")
}

func outcomeName(reason react.StopReason) string {
	switch reason {
	case react.StopCompleted:
		return "completed"
	case react.StopInterrupted:
		return "cancelled"
	case react.StopBlocked, react.StopStalled, react.StopBudgetExhausted:
		return "partial"
	default:
		return "failed"
	}
}

func foldSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maximum = 256
	if len(value) <= maximum {
		return value
	}
	return fmt.Sprintf("%s...", value[:maximum-3])
}
