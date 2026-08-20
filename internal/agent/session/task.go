package session

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type TaskOutput struct {
	Items         []rollout.RolloutItem
	Summary       string
	Outcome       protocol.TurnOutcome
	Reason        string
	Usage         llm.Usage
	ToolCallCount int
}

type SessionTask interface {
	Run(context.Context, *Session, *turn.TurnContext) (TaskOutput, error)
}

type regularTask struct {
	runtime      *SessionServices
	goal         string
	events       protocol.EventSink
	instructions *targetInstructionScope
}

func (sessionTask *regularTask) Run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (TaskOutput, error) {
	return sessionTask.run(ctx, session, turnContext)
}

type compactTask struct {
	runtime *SessionServices
	events  protocol.EventSink
}
