package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (sessionTask *compactTask) Run(ctx context.Context, session *Session, _ *turn.TurnContext, _ []TurnInput) (Result, error) {
	if sessionTask == nil || sessionTask.runtime == nil {
		return Result{}, errors.New("compact task is nil")
	}
	items, err := sessionTask.runtime.Compact(ctx, session.History())
	if err != nil {
		return Result{}, err
	}
	return Result{Items: items, Summary: "result: completed", Outcome: OutcomeCompleted}, nil
}
