package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type Completion struct {
	TurnID protocol.TurnID
	Output TaskOutput
	Error  error
	Cause  error
}

type RunningTask struct {
	mu      sync.Mutex
	task    SessionTask
	kind    TaskKind
	context *TurnContext
	session *Session
	ctx     context.Context
	cancel  context.CancelCauseFunc
	done    chan Completion
	started bool
}

func NewRunningTask(parent context.Context, session *Session, sessionTask SessionTask, turnContext *TurnContext) (*RunningTask, error) {
	if parent == nil || session == nil || sessionTask == nil || turnContext == nil {
		return nil, errors.New("running task is incomplete")
	}
	if err := turnContext.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &RunningTask{
		task: sessionTask, kind: sessionTask.Kind(), context: turnContext, session: session,
		ctx: ctx, cancel: cancel, done: make(chan Completion, 1),
	}, nil
}

func (running *RunningTask) Kind() TaskKind {
	if running == nil {
		return ""
	}
	return running.kind
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
		completion.Output, completion.Error = running.task.Run(running.ctx, running.session, running.context)
	}()
	return running.done
}

func (running *RunningTask) Cancel(cause error) {
	if running == nil {
		return
	}
	running.cancel(cause)
}

func (running *RunningTask) Context() *TurnContext {
	if running == nil {
		return nil
	}
	return running.context
}
