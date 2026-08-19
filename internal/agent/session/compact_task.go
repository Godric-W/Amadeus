package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (sessionTask *compactTask) Run(ctx context.Context, session *Session, turnContext *turn.TurnContext, _ []TurnInput) (Result, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.events == nil || session == nil || turnContext == nil {
		return Result{}, errors.New("compact task is nil")
	}
	modelSession, err := sessionTask.runtime.NewModelClientSession()
	if err != nil {
		return Result{}, err
	}
	items, err := sessionTask.runtime.Compact(ctx, engine.CompactRequest{Lines: session.History(), ModelSession: modelSession, Events: sessionTask.events})
	if err != nil {
		return Result{}, err
	}
	return Result{Items: items, Summary: "result: completed", Outcome: OutcomeCompleted}, nil
}
