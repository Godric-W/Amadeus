package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (session *Session) runTurn(ctx context.Context, runtime *engine.Services, turnContext turn.TurnContext, inputs []TurnInput, events protocol.EventSink, instructions engine.StepInstructionScope) (engine.RunResult, error) {
	if session == nil || runtime == nil || events == nil || instructions == nil {
		return engine.RunResult{}, errors.New("session turn execution is incomplete")
	}
	return session.runTurnLoop(ctx, runtime, turnContext, inputs, events, instructions)
}
