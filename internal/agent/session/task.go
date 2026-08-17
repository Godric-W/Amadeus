package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type TaskKind string

const (
	TaskKindRegular TaskKind = "regular"
	TaskKindCompact TaskKind = "compact"
)

type TurnInput struct {
	Content string
}

type Outcome = rollout.TurnOutcome

const (
	OutcomeCompleted = rollout.TurnOutcomeCompleted
	OutcomeBlocked   = rollout.TurnOutcomeBlocked
	OutcomeFailed    = rollout.TurnOutcomeFailed
	OutcomeAborted   = rollout.TurnOutcomeAborted
)

type Result struct {
	Items         []rollout.Item
	Summary       string
	Outcome       Outcome
	Reason        string
	Usage         llm.Usage
	ToolCallCount int
}

type SessionTask interface {
	Kind() TaskKind
	Run(context.Context, *Session, *turn.TurnContext, []TurnInput) (Result, error)
	Abort(context.Context, *Session, *turn.TurnContext) error
}

type TaskConstructors struct {
	Regular func(context.Context, *Session, string, turn.TurnContext) (SessionTask, turn.TurnContext, error)
	Compact func(context.Context, *Session, string, turn.TurnContext) (SessionTask, turn.TurnContext, error)
	Close   func() error
}

type SessionSetup struct {
	TaskConstructors TaskConstructors
	BuildServices    func(context.Context, *Session) (*engine.Services, error)
}

func (constructors TaskConstructors) Valid() bool {
	return constructors.Regular != nil && constructors.Compact != nil && constructors.Close != nil
}

type FuncTask struct {
	TaskKind  TaskKind
	RunFunc   func(context.Context, *Session, *turn.TurnContext, []TurnInput) (Result, error)
	AbortFunc func(context.Context, *Session, *turn.TurnContext) error
}

func (value FuncTask) Kind() TaskKind {
	return value.TaskKind
}

func (value FuncTask) Run(ctx context.Context, session *Session, turnContext *turn.TurnContext, inputs []TurnInput) (Result, error) {
	if value.RunFunc == nil {
		return Result{}, errors.New("session task run function is nil")
	}
	return value.RunFunc(ctx, session, turnContext, inputs)
}

func (value FuncTask) Abort(ctx context.Context, session *Session, turnContext *turn.TurnContext) error {
	if value.AbortFunc == nil {
		return nil
	}
	return value.AbortFunc(ctx, session, turnContext)
}
