package task

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type Kind string

const (
	KindRegular Kind = "regular"
	KindCompact Kind = "compact"
)

type Input struct {
	Content string
}

type Result struct {
	Items   []rollout.Item
	Summary string
}

type Host interface {
	AppendItems(context.Context, turn.ID, ...rollout.Item) error
	History() []rollout.Line
}

type PlanHost interface {
	Host
	UpdatePlan(context.Context, turn.ID, plan.Update) (plan.Snapshot, error)
}

type ContextHost interface {
	Host
	Context() *agentcontext.Manager
}

type SessionTask interface {
	Kind() Kind
	Run(context.Context, Host, *turn.Context, []Input) (Result, error)
	Abort(context.Context, Host, *turn.Context) error
}

type PrepareRequest struct {
	Kind    Kind
	Input   string
	Context turn.Context
}

type Prepared struct {
	Task    SessionTask
	Context turn.Context
}

type Factory interface {
	Prepare(context.Context, Host, PrepareRequest) (Prepared, error)
}

type FuncTask struct {
	TaskKind  Kind
	RunFunc   func(context.Context, Host, *turn.Context, []Input) (Result, error)
	AbortFunc func(context.Context, Host, *turn.Context) error
}

func (value FuncTask) Kind() Kind {
	return value.TaskKind
}

func (value FuncTask) Run(ctx context.Context, host Host, turnContext *turn.Context, inputs []Input) (Result, error) {
	if value.RunFunc == nil {
		return Result{}, errors.New("session task run function is nil")
	}
	return value.RunFunc(ctx, host, turnContext, inputs)
}

func (value FuncTask) Abort(ctx context.Context, host Host, turnContext *turn.Context) error {
	if value.AbortFunc == nil {
		return nil
	}
	return value.AbortFunc(ctx, host, turnContext)
}
