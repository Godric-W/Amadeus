package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/config"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func (runner *agentController) executeReactorRun(ctx context.Context, runRuntime *agentruntime.RunRuntime, invocation agentInvocation, configured config.Config, agent *bootstrap.Agent, requestViewProvider react.RequestViewProvider) (bool, error) {
	if err := agent.Events.Publish(ctx, event.RunStarted{}); err != nil {
		return false, fmt.Errorf("publish Reactor Run started: %w", err)
	}
	if err := agent.Events.Publish(ctx, event.RunStatusChanged{Entity: "run", EntityID: string(runRuntime.RunContext().Run.ID), From: "", To: "running"}); err != nil {
		return false, fmt.Errorf("publish Reactor Run status: %w", err)
	}
	result, runErr := agent.Runner.Run(ctx, react.Request{
		RunID: string(runRuntime.RunContext().Run.ID), Goal: invocation.Task,
		RequestViewProvider: requestViewProvider, Budget: configuredReactorBudget(configured.Agent),
	})
	status, stopReason := persistentReactorOutcome(result, runErr, ctx.Err())
	if publishErr := agent.Events.Publish(context.WithoutCancel(ctx), event.RunCompleted{
		Status: string(status), StopReason: string(result.StopReason), Reason: reactorRunEventReason(status, stopReason, result.Reason),
	}); publishErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("publish Reactor Run completed: %w", publishErr))
	}
	runRuntime.State().AddUsage(result.Usage)
	if stateErr := runRuntime.State().AddToolCalls(result.Budget.ToolCallsUsed); stateErr != nil {
		runErr = errors.Join(runErr, stateErr)
	}
	assistantContent := ""
	if status == sessiondomain.RunCompleted && result.FinalMessage != nil {
		assistantContent = strings.TrimSpace(result.FinalMessage.Content)
	}
	if finishErr := runRuntime.Finish(context.WithoutCancel(ctx), status, stopReason, assistantContent); finishErr != nil {
		return false, errors.Join(runErr, finishErr)
	}
	if runErr != nil {
		return true, runErr
	}
	outcome, code := classifyReactorResult(result)
	summary := formatReactorSummary(outcome, result)
	fmt.Fprintln(invocation.ErrorOutput, summary)
	if code != exitCodeSuccess {
		return true, &commandExitError{code: code, message: summary, reported: true}
	}
	return true, nil
}

func persistentReactorOutcome(result react.Result, runErr, contextErr error) (sessiondomain.RunStatus, string) {
	if errors.Is(contextErr, context.Canceled) || result.StopReason == react.StopInterrupted {
		return sessiondomain.RunInterrupted, "user cancelled"
	}
	if runErr != nil {
		return sessiondomain.RunFailed, foldSummary(runErr.Error())
	}
	if result.StopReason == react.StopCompleted {
		return sessiondomain.RunCompleted, ""
	}
	return sessiondomain.RunFailed, nonEmptyStopReason(result.Reason, string(result.StopReason))
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
	return "run did not complete"
}

func reactorRunEventReason(status sessiondomain.RunStatus, values ...string) string {
	if status == sessiondomain.RunCompleted {
		return ""
	}
	return nonEmptyStopReason(values...)
}

func configuredReactorBudget(agent config.AgentConfig) react.BudgetState {
	return react.BudgetState{Budget: react.Budget{
		MaxIterations: agent.MaxIterations, MaxToolCalls: agent.MaxToolCalls,
		MaxInputTokens: agent.MaxInputTokens, MaxOutputTokens: agent.MaxOutputTokens,
		MaxDuration: agent.MaxDuration,
	}}
}
