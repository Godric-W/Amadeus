package task

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (sessionTask *regularTask) executeReactor(ctx context.Context, agent *agentruntime.Agent, contextManager *agentcontext.Manager, compact func(context.Context) error, availableTools []tool.ToolSpec, outputSchema llm.OutputSchema, turnID rollout.TurnID) (Result, error) {
	provider := sessionTask.factory.configured.Providers[sessionTask.factory.configured.DefaultProvider]
	modelInfo := normalizedModelInfo(agent.Client.Model(), provider)
	result, runErr := agent.Runner.Run(ctx, react.Request{
		TurnID: string(turnID), Goal: sessionTask.goal, Context: contextManager,
		BaseInstructions: agent.BaseInstructions,
		ModelInfo:        modelInfo, AvailableTools: availableTools, OutputSchema: append(llm.OutputSchema(nil), outputSchema...),
		BeforeSample: func(sampleCtx context.Context, _ *agentcontext.Manager) error {
			if compact == nil {
				return nil
			}
			return compact(sampleCtx)
		},
		Budget: configuredReactorBudget(sessionTask.factory.configured.Agent),
	})
	items := make([]rollout.Item, 0, 2)
	if result.FinalMessage != nil && (strings.TrimSpace(result.FinalMessage.Content) != "" || strings.TrimSpace(result.FinalMessage.Reasoning) != "") {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{
			Type: rollout.ResponseAssistantMessage, Role: string(llm.RoleAssistant),
			Content: strings.TrimSpace(result.FinalMessage.Content), Reasoning: strings.TrimSpace(result.FinalMessage.Reasoning),
		})
		if err != nil {
			runErr = errors.Join(runErr, err)
		} else {
			items = append(items, item)
		}
	}
	usage, err := rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{
		InputTokens: result.Usage.InputTokens, CachedInputTokens: result.Usage.CachedInputTokens,
		OutputTokens: result.Usage.OutputTokens, ReasoningTokens: result.Usage.ReasoningTokens,
		TotalTokens: int64(result.Usage.TotalTokens),
	})
	if err != nil {
		runErr = errors.Join(runErr, err)
	} else {
		items = append(items, usage)
	}
	if runErr != nil {
		return Result{Items: items, Summary: "result: failed"}, runErr
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return Result{Items: items, Summary: "result: cancelled"}, ctx.Err()
	}
	if result.StopReason != react.StopCompleted {
		outcomeErr := &OutcomeError{StopReason: result.StopReason, Reason: result.Reason}
		return Result{Items: items, Summary: outcomeErr.Error()}, outcomeErr
	}
	return Result{Items: items, Summary: "result: completed"}, nil
}

func configuredReactorBudget(agent config.AgentConfig) react.BudgetState {
	return react.BudgetState{Budget: react.Budget{MaxIterations: agent.MaxIterations, MaxToolCalls: agent.MaxToolCalls, MaxDuration: agent.MaxDuration}}
}
