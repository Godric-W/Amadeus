package session

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

type TaskOutput struct {
	Summary       string
	Outcome       protocol.TurnOutcome
	Reason        string
	ToolCallCount int
}

type SessionTask interface {
	Kind() TaskKind
	Run(context.Context, *Session, *turn.TurnContext) (TaskOutput, error)
}

type regularTask struct {
	runtime      *SessionServices
	goal         string
	events       protocol.EventSink
	turnState    *TurnState
	modelSession *engine.ModelClientSession
	initialized  bool
}

func (sessionTask *regularTask) Run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (TaskOutput, error) {
	return sessionTask.run(ctx, session, turnContext)
}

func (*regularTask) Kind() TaskKind { return TaskKindRegular }

type compactTask struct {
	runtime *SessionServices
	events  protocol.EventSink
}

func (*compactTask) Kind() TaskKind { return TaskKindCompact }
