package task

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

type Completion struct {
	TurnID turn.ID
	Result Result
	Error  error
	Cause  error
}

type RunningTask struct {
	mu        sync.Mutex
	task      SessionTask
	context   *turn.Context
	host      Host
	inputs    []Input
	ctx       context.Context
	cancel    context.CancelCauseFunc
	done      chan Completion
	started   bool
	abortOnce sync.Once
}

func NewRunningTask(parent context.Context, host Host, sessionTask SessionTask, turnContext *turn.Context, inputs []Input) (*RunningTask, error) {
	if parent == nil || host == nil || sessionTask == nil || turnContext == nil {
		return nil, errors.New("running task is incomplete")
	}
	if err := turnContext.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &RunningTask{
		task: sessionTask, context: turnContext, host: host, inputs: append([]Input(nil), inputs...),
		ctx: ctx, cancel: cancel, done: make(chan Completion, 1),
	}, nil
}

func (running *RunningTask) Start() <-chan Completion {
	running.mu.Lock()
	if running.started {
		running.mu.Unlock()
		return running.done
	}
	running.started = true
	running.mu.Unlock()
	go func() {
		completion := Completion{TurnID: running.context.TurnID}
		defer func() {
			if recovered := recover(); recovered != nil {
				completion.Error = fmt.Errorf("session task panicked: %v", recovered)
			}
			completion.Cause = context.Cause(running.ctx)
			running.done <- completion
			close(running.done)
		}()
		completion.Result, completion.Error = running.task.Run(running.ctx, running.host, running.context, running.inputs)
	}()
	return running.done
}

func (running *RunningTask) Cancel(cause error) {
	if running == nil {
		return
	}
	running.cancel(cause)
}

func (running *RunningTask) Abort(ctx context.Context) error {
	if running == nil {
		return nil
	}
	var abortErr error
	running.abortOnce.Do(func() {
		abortErr = running.task.Abort(ctx, running.host, running.context)
	})
	return abortErr
}

func (running *RunningTask) Context() *turn.Context {
	if running == nil {
		return nil
	}
	return running.context
}
