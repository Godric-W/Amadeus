package goal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
)

type RuntimeHandle struct {
	threadID       protocol.ThreadID
	store          state.GoalStore
	automation     agentsession.ThreadAutomation
	events         extension.EventSink
	accounting     *accountingState
	stateLock      chan struct{}
	enabled        atomic.Bool
	toolsAvailable bool
}

func newRuntimeHandle(threadID protocol.ThreadID, store state.GoalStore, automation agentsession.ThreadAutomation, events extension.EventSink, accounting *accountingState, enabled, toolsAvailable bool) *RuntimeHandle {
	lock := make(chan struct{}, 1)
	lock <- struct{}{}
	if events == nil {
		events = extension.NoopEventSink{}
	}
	runtime := &RuntimeHandle{threadID: threadID, store: store, automation: automation, events: events, accounting: accounting, stateLock: lock, toolsAvailable: toolsAvailable}
	runtime.enabled.Store(enabled)
	return runtime
}

func (runtime *RuntimeHandle) acquireGoalState(ctx context.Context) (func(), error) {
	select {
	case <-runtime.stateLock:
		return func() { runtime.stateLock <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (runtime *RuntimeHandle) setEnabled(enabled bool) { runtime.enabled.Store(enabled) }

func (runtime *RuntimeHandle) toolsVisible() bool {
	return runtime.enabled.Load() && runtime.toolsAvailable
}

func (runtime *RuntimeHandle) restoreAfterResume(ctx context.Context) error {
	if !runtime.enabled.Load() {
		return nil
	}
	goal, err := runtime.store.Get(ctx, runtime.threadID)
	if err != nil {
		return err
	}
	if goal != nil && goal.Status == protocol.ThreadGoalActive {
		runtime.accounting.markIdleGoalActive(goal.GoalID)
	} else {
		runtime.accounting.clearActiveGoal()
	}
	return nil
}

func (runtime *RuntimeHandle) prepareExternalMutation(ctx context.Context) error {
	if !runtime.enabled.Load() {
		return nil
	}
	if turnID := runtime.accounting.currentTurnID(); turnID != "" {
		_, err := runtime.accountTurnProgress(ctx, turnID, state.GoalAccountingActiveOnly, false, string(turnID)+":external-goal-mutation")
		return err
	}
	_, err := runtime.accountIdleProgress(ctx, state.GoalAccountingActiveOnly, false, runtime.threadID.String()+":external-goal-mutation")
	return err
}

func (runtime *RuntimeHandle) applyExternalSet(ctx context.Context, goal state.ThreadGoal, previous *previousGoal) error {
	if !runtime.enabled.Load() {
		return nil
	}
	objectiveChanged := previous != nil && previous.GoalID == goal.GoalID && previous.Objective != goal.Objective
	switch goal.Status {
	case protocol.ThreadGoalActive:
		if runtime.accounting.currentTurnID() != "" {
			runtime.accounting.markCurrentGoalActive(goal.GoalID)
		} else {
			runtime.accounting.markIdleGoalActive(goal.GoalID)
		}
		if objectiveChanged {
			input, err := objectiveUpdatedInput(goal.Protocol())
			if err != nil {
				return err
			}
			if err := runtime.automation.InjectIfRunning(ctx, input); err != nil && !strings.Contains(err.Error(), "no active turn") {
				return err
			}
		}
		return runtime.continueIfIdle(ctx)
	case protocol.ThreadGoalBudgetLimited:
		if runtime.accounting.currentTurnID() == "" {
			runtime.accounting.clearActiveGoal()
		}
	default:
		runtime.accounting.clearActiveGoal()
	}
	return nil
}

func (runtime *RuntimeHandle) continueIfIdle(ctx context.Context) error {
	if !runtime.toolsVisible() {
		runtime.accounting.clearActiveGoal()
		return nil
	}
	release, err := runtime.acquireGoalState(ctx)
	if err != nil {
		return err
	}
	defer release()
	deferred, err := runtime.store.HasContinuationDeferral(ctx, runtime.threadID)
	if err != nil || deferred {
		return err
	}
	goal, err := runtime.store.Get(ctx, runtime.threadID)
	if err != nil {
		return err
	}
	if goal == nil || goal.Status != protocol.ThreadGoalActive {
		runtime.accounting.clearActiveGoal()
		return nil
	}
	input, err := continuationInput(goal.Protocol())
	if err != nil {
		return err
	}
	submission, err := runtime.automation.StartTurnIfIdle(ctx, input)
	if err == nil && !submission.Started() {
		turnID := runtime.accounting.currentTurnID()
		if turnID == "" || !runtime.accounting.currentTurnOwnsGoal(turnID) {
			runtime.accounting.clearActiveGoal()
		}
	}
	return err
}

func (runtime *RuntimeHandle) stopGoalForTurn(ctx context.Context, turnID protocol.TurnID, cause error) error {
	if !runtime.enabled.Load() || !runtime.accounting.currentTurnOwnsGoal(turnID) {
		return nil
	}
	release, err := runtime.acquireGoalState(ctx)
	if err != nil {
		return err
	}
	defer release()
	if _, err := runtime.accountTurnProgress(ctx, turnID, state.GoalAccountingActiveOnly, false, string(turnID)+":turn-error-progress"); err != nil {
		return err
	}
	goal, err := runtime.store.Get(ctx, runtime.threadID)
	if err != nil || goal == nil {
		runtime.accounting.clearActiveGoal()
		return err
	}
	status := protocol.ThreadGoalBlocked
	if provider, ok := llm.AsProviderError(cause); ok && provider.Kind == llm.ProviderErrorRateLimit {
		status = protocol.ThreadGoalUsageLimited
	}
	if goal.Status != protocol.ThreadGoalActive && !(goal.Status == protocol.ThreadGoalBudgetLimited && status == protocol.ThreadGoalUsageLimited) {
		runtime.accounting.clearActiveGoal()
		return nil
	}
	updated, err := runtime.store.Update(ctx, runtime.threadID, state.GoalUpdate{Status: &status, ExpectedGoalID: goal.GoalID})
	if err != nil || updated == nil {
		return err
	}
	runtime.accounting.clearActiveGoal()
	return runtime.publishGoal(ctx, string(turnID)+":turn-error", turnID, *updated)
}

func (runtime *RuntimeHandle) accountTurnProgress(ctx context.Context, turnID protocol.TurnID, mode state.GoalAccountingMode, keepBudgetLimitedActive bool, eventID string) (*state.ThreadGoal, error) {
	release, err := runtime.accounting.acquireProgress(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	snapshot := runtime.accounting.snapshot(turnID)
	if snapshot == nil {
		return nil, nil
	}
	outcome, err := runtime.store.AccountUsage(ctx, runtime.threadID, snapshot.timeDelta, snapshot.tokenDelta, mode, snapshot.goalID)
	if err != nil || !outcome.Updated || outcome.Goal == nil {
		return nil, err
	}
	runtime.accounting.markAccounted(turnID, snapshot, outcome.Goal.Status, keepBudgetLimitedActive)
	if err := runtime.publishGoal(ctx, eventID, turnID, *outcome.Goal); err != nil {
		return nil, err
	}
	return outcome.Goal, nil
}

func (runtime *RuntimeHandle) accountIdleProgress(ctx context.Context, mode state.GoalAccountingMode, keepBudgetLimitedActive bool, eventID string) (*state.ThreadGoal, error) {
	release, err := runtime.accounting.acquireProgress(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	snapshot := runtime.accounting.idleSnapshot()
	if snapshot == nil {
		return nil, nil
	}
	outcome, err := runtime.store.AccountUsage(ctx, runtime.threadID, snapshot.timeDelta, 0, mode, snapshot.goalID)
	if err != nil || !outcome.Updated || outcome.Goal == nil {
		return nil, err
	}
	runtime.accounting.markIdleAccounted(snapshot, outcome.Goal.Status, keepBudgetLimitedActive)
	if err := runtime.publishGoal(ctx, eventID, "", *outcome.Goal); err != nil {
		return nil, err
	}
	return outcome.Goal, nil
}

func (runtime *RuntimeHandle) publishGoal(ctx context.Context, eventID string, turnID protocol.TurnID, goal state.ThreadGoal) error {
	if strings.TrimSpace(eventID) == "" {
		return errors.New("goal event ID is empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.events.Emit(protocol.Event{ID: protocol.EventID(eventID), Msg: protocol.ThreadGoalUpdatedEvent{ThreadID: runtime.threadID, TurnID: turnID, Goal: goal.Protocol()}})
	return nil
}

func (runtime *RuntimeHandle) publishOrdered(ctx context.Context, event protocol.Event) error {
	if ordered, ok := runtime.events.(extension.OrderedEventSink); ok {
		return ordered.EmitOrdered(ctx, event)
	}
	runtime.events.Emit(event)
	return nil
}

func (runtime *RuntimeHandle) injectBudgetLimit(ctx context.Context, goal state.ThreadGoal) error {
	if !runtime.accounting.markBudgetReported(goal.GoalID) {
		return nil
	}
	input, err := budgetLimitInput(goal.Protocol())
	if err != nil {
		return err
	}
	if err := runtime.automation.InjectIfRunning(ctx, input); err != nil {
		return fmt.Errorf("inject goal budget limit: %w", err)
	}
	return nil
}
