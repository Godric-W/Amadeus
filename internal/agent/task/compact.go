package task

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (sessionTask *compactTask) Run(ctx context.Context, host Host, _ *turn.Context, _ []Input) (Result, error) {
	if sessionTask == nil || sessionTask.factory == nil {
		return Result{}, errors.New("compact task is nil")
	}
	runtime, err := sessionTask.factory.ensureRuntime(host)
	if err != nil {
		return Result{}, err
	}
	items, err := runtime.Compact(ctx, host.History())
	if err != nil {
		return Result{}, err
	}
	return Result{Items: items, Summary: "result: completed", Outcome: OutcomeCompleted}, nil
}
