package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (session *Session) runTurn(ctx context.Context, runtime *SessionServices, turnContext turn.TurnContext, events protocol.EventSink, instructions engine.StepInstructionScope) (TaskOutput, error) {
	if session == nil || runtime == nil || events == nil || instructions == nil {
		return TaskOutput{}, errors.New("session turn execution is incomplete")
	}
	return session.runTurnLoop(ctx, runtime, turnContext, events, instructions)
}
