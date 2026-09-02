package session

import (
	"context"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/modelclient"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

type TaskOutput struct {
	Summary          string
	Outcome          protocol.TurnOutcome
	Reason           string
	LastAgentMessage *string
	ToolCallCount    int
}

type SessionTask interface {
	Kind() TaskKind
	Run(context.Context, *Session, *TurnContext) (TaskOutput, error)
}

type regularTask struct {
	runtime      *SessionServices
	initialInput TurnInput
	startedAt    time.Time
	events       protocol.EventSink
	turnState    *TurnState
	modelSession *modelclient.ModelClientSession
	initialized  bool
}

func (sessionTask *regularTask) Run(ctx context.Context, session *Session, turnContext *TurnContext) (TaskOutput, error) {
	return sessionTask.run(ctx, session, turnContext)
}

func (*regularTask) Kind() TaskKind { return TaskKindRegular }

type compactTask struct {
	runtime *SessionServices
	events  protocol.EventSink
}

func (*compactTask) Kind() TaskKind { return TaskKindCompact }
