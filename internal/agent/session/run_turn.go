package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (session *Session) runTurn(ctx context.Context, runtime *SessionServices, modelSession *engine.ModelClientSession, turnContext turn.TurnContext, state *TurnState, events protocol.EventSink, canDrainPendingInput bool) (TaskOutput, error) {
	if session == nil || runtime == nil || modelSession == nil || state == nil || events == nil {
		return TaskOutput{}, errors.New("session turn execution is incomplete")
	}
	compact := session.compactCallback(runtime, modelSession, turnContext.TurnID, events)
	return session.continueTurn(ctx, runtime, modelSession, turnContext, state, events, compact, canDrainPendingInput)
}
