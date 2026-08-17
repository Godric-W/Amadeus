package task

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (factory *CodingFactory) prepareRegular(_ context.Context, host Host, snapshot turn.Context, goal string) (Prepared, error) {
	runtime, err := factory.ensureRuntime(host)
	if err != nil {
		return Prepared{}, err
	}
	runtimeHost, ok := host.(eventRequestHost)
	if !ok {
		return Prepared{}, errors.New("session task host does not expose event and request boundaries")
	}
	events, err := protocol.NewScopedSink(runtimeHost, snapshot.ThreadID, snapshot.TurnID)
	if err != nil {
		return Prepared{}, err
	}
	instructionScope, err := newTargetInstructionScope(host, factory.instructions, snapshot.TurnID)
	if err != nil {
		return Prepared{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return Prepared{}, err
	}
	return Prepared{Task: &regularTask{runtime: runtime, goal: goal, events: events, instructions: instructionScope}, Context: snapshot}, nil
}

func (sessionTask *regularTask) run(ctx context.Context, host Host, turnContext *turn.Context) (Result, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.instructions == nil || sessionTask.events == nil {
		return Result{}, errors.New("regular task is nil")
	}
	for _, warning := range sessionTask.runtime.SkillWarnings() {
		if warning != nil {
			if err := sessionTask.events.Publish(ctx, protocol.SessionEvent{Message: protocol.Warning{Message: warning.Error()}}); err != nil {
				return Result{}, err
			}
		}
	}
	promptHost, ok := host.(PromptHost)
	if !ok {
		return Result{}, errors.New("session task host does not expose prompt snapshots")
	}
	if err := sessionTask.runtime.PrepareTurn(ctx, sessionTask.goal, promptHost, turnContext, sessionTask.instructions); err != nil {
		return Result{}, err
	}
	autoCompact := func(compactCtx context.Context) (bool, error) {
		items, compactErr := sessionTask.runtime.Compact(compactCtx, host.History())
		if compactErr != nil {
			if strings.Contains(compactErr.Error(), "no earlier turn") || strings.Contains(compactErr.Error(), "no safely compactable") || strings.Contains(compactErr.Error(), "no conversation") {
				return false, nil
			}
			return false, compactErr
		}
		if len(items) == 0 {
			return false, nil
		}
		if err := host.AppendItems(compactCtx, turnContext.TurnID, items...); err != nil {
			return false, err
		}
		return true, nil
	}
	runResult, err := sessionTask.runtime.RunTurn(ctx, engine.RunRequest{
		Host: promptHost, Turn: *turnContext,
		Events: sessionTask.events, Instructions: sessionTask.instructions, Compact: autoCompact,
	})
	usageItem, usageErr := engine.UsageItem(runResult.Usage)
	items := make([]rollout.Item, 0, 1)
	if usageErr == nil {
		items = append(items, usageItem)
	}
	if err != nil {
		if ctx.Err() != nil {
			return Result{Items: items, Summary: "result: cancelled", Outcome: OutcomeAborted, Reason: context.Cause(ctx).Error(), Usage: runResult.Usage, ToolCallCount: runResult.ToolCallCount}, errors.Join(err, usageErr)
		}
		return Result{Items: items, Summary: runResult.Summary, Outcome: OutcomeFailed, Reason: err.Error(), Usage: runResult.Usage, ToolCallCount: runResult.ToolCallCount}, errors.Join(err, usageErr)
	}
	outcome := runResult.Outcome
	if !outcome.Valid() {
		outcome = OutcomeCompleted
	}
	return Result{Items: items, Summary: runResult.Summary, Outcome: outcome, Reason: runResult.Reason, Usage: runResult.Usage, ToolCallCount: runResult.ToolCallCount}, usageErr
}
