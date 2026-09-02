package app

import (
	"context"
	"errors"

	goalextension "github.com/Godric-W/Amadeus/internal/extension/goal"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
)

func (application *InteractiveApplication) Goal(ctx context.Context) (*protocol.ThreadGoal, error) {
	if !application.configuration.Runtime.Features.Goals {
		return nil, errors.New("goals feature is disabled")
	}
	active, _, err := application.current()
	if err != nil {
		return nil, err
	}
	goal, err := application.workspace.GetGoal(ctx, active.ID())
	if err != nil || goal == nil {
		return goal, err
	}
	goal.Objective = expandGoalObjective(application.configuration.AmadeusRoot, goal.Objective)
	return goal, nil
}

func (application *InteractiveApplication) SetGoal(ctx context.Context, objective goalextension.ObjectiveUpdate, status *protocol.ThreadGoalStatus, budget state.TokenBudgetUpdate) (protocol.ThreadGoal, error) {
	if !application.configuration.Runtime.Features.Goals {
		return protocol.ThreadGoal{}, errors.New("goals feature is disabled")
	}
	active, _, err := application.current()
	if err != nil {
		return protocol.ThreadGoal{}, err
	}
	cleanup := func() {}
	previous, _ := application.workspace.GetGoal(ctx, active.ID())
	if objective.Set {
		materialized, remove, err := materializeGoalObjective(application.configuration.AmadeusRoot, objective.Value)
		if err != nil {
			return protocol.ThreadGoal{}, err
		}
		objective.Value = materialized
		cleanup = remove
	}
	maximum := application.configuration.Runtime.Goals.MaxGoalTokenBudget
	goal, err := application.workspace.SetGoal(ctx, goalextension.SetRequest{
		ThreadID: active.ID(), Objective: objective, Status: status, TokenBudget: budget, MaxGoalTokenBudget: maximum,
	})
	if err != nil {
		cleanup()
		return protocol.ThreadGoal{}, err
	}
	if previous != nil && objective.Set && previous.Objective != goal.Objective {
		cleanupGoalObjectiveFile(application.configuration.AmadeusRoot, previous.Objective)
	}
	return goal, nil
}

func (application *InteractiveApplication) ClearGoal(ctx context.Context) (bool, error) {
	if !application.configuration.Runtime.Features.Goals {
		return false, errors.New("goals feature is disabled")
	}
	active, _, err := application.current()
	if err != nil {
		return false, err
	}
	previous, _ := application.workspace.GetGoal(ctx, active.ID())
	cleared, err := application.workspace.ClearGoal(ctx, active.ID())
	if err == nil && cleared && previous != nil {
		cleanupGoalObjectiveFile(application.configuration.AmadeusRoot, previous.Objective)
	}
	return cleared, err
}

func (application *InteractiveApplication) PauseGoalAndInterrupt(ctx context.Context) error {
	goal, err := application.Goal(ctx)
	if err != nil {
		return err
	}
	if goal == nil || goal.Status != protocol.ThreadGoalActive {
		return application.Interrupt(ctx)
	}
	paused := protocol.ThreadGoalPaused
	if _, err := application.SetGoal(ctx, goalextension.ObjectiveUpdate{}, &paused, state.TokenBudgetUpdate{}); err != nil {
		return errors.Join(errors.New("pause active goal before interrupt"), err)
	}
	return application.Interrupt(ctx)
}
