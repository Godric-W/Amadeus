package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/agent/task"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (runner *agentController) executeReactorTurn(ctx context.Context, invocation agentInvocation, configured config.Config, agent *bootstrap.Agent, contextManager *agentcontext.Manager, compact func(context.Context) error, availableTools []tool.ToolSpec, outputSchema llm.OutputSchema, turnID rollout.TurnID) (task.Result, error) {
	provider := configured.Providers[configured.DefaultProvider]
	modelInfo := agent.Client.Model()
	modelInfo.ContextWindow = provider.ContextWindow
	modelInfo.MaxOutputTokens = provider.MaxOutputTokens
	modelInfo.AutoCompactTokenLimit = provider.AutoCompactTokenLimit
	modelInfo.ToolOutputMaxTokens = provider.ToolOutputMaxTokens
	modelInfo = modelInfo.Normalized()
	result, runErr := agent.Runner.Run(ctx, react.Request{
		TurnID: string(turnID), Goal: invocation.Task, Context: contextManager,
		BaseInstructions: agent.BaseInstructions,
		ModelInfo:        modelInfo, AvailableTools: availableTools, OutputSchema: append(llm.OutputSchema(nil), outputSchema...),
		BeforeSample: func(sampleCtx context.Context, _ *agentcontext.Manager) error {
			if compact == nil {
				return nil
			}
			return compact(sampleCtx)
		},
		Budget: configuredReactorBudget(configured.Agent),
	})
	outcome, code := classifyReactorResult(result)
	if errors.Is(ctx.Err(), context.Canceled) || result.StopReason == react.StopInterrupted {
		outcome, code = runOutcomeCancelled, exitCodeCancelled
	}
	items := make([]rollout.Item, 0, 2)
	if result.FinalMessage != nil && (strings.TrimSpace(result.FinalMessage.Content) != "" || strings.TrimSpace(result.FinalMessage.Reasoning) != "") {
		payload := map[string]any{"type": "assistant_message", "role": "assistant", "content": strings.TrimSpace(result.FinalMessage.Content)}
		if reasoning := strings.TrimSpace(result.FinalMessage.Reasoning); reasoning != "" {
			payload["reasoning_content"] = reasoning
		}
		item, err := newResponseItem(payload)
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
		return task.Result{Items: items}, runErr
	}
	summary := formatReactorSummary(outcome, result)
	fmt.Fprintln(invocation.ErrorOutput, summary)
	if code != exitCodeSuccess {
		return task.Result{Items: items}, &commandExitError{code: code, message: summary, reported: true}
	}
	return task.Result{Items: items}, nil
}

func turnTerminalStatus(outcome runOutcome) rollout.TurnTerminalStatus {
	switch outcome {
	case runOutcomeCompleted:
		return rollout.TurnStatusCompleted
	default:
		return rollout.TurnStatusFailed
	}
}

func classifyReactorResult(result react.Result) (runOutcome, int) {
	switch result.StopReason {
	case react.StopCompleted:
		return runOutcomeCompleted, exitCodeSuccess
	case react.StopInterrupted:
		return runOutcomeCancelled, exitCodeCancelled
	case react.StopBlocked, react.StopStalled:
		return runOutcomePartial, exitCodePartial
	default:
		return runOutcomeFailed, exitCodeFailure
	}
}

func formatReactorSummary(outcome runOutcome, result react.Result) string {
	parts := []string{"result: " + string(outcome)}
	if result.StopReason != "" {
		parts = append(parts, "stop_reason="+string(result.StopReason))
	}
	if reason := strings.TrimSpace(result.Reason); reason != "" {
		parts = append(parts, "reason="+foldSummary(reason))
	}
	return strings.Join(parts, " ")
}

func nonEmptyStopReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return foldSummary(value)
		}
	}
	return "turn did not complete"
}

func configuredReactorBudget(agent config.AgentConfig) react.BudgetState {
	return react.BudgetState{Budget: react.Budget{MaxIterations: agent.MaxIterations, MaxToolCalls: agent.MaxToolCalls, MaxDuration: agent.MaxDuration}}
}
