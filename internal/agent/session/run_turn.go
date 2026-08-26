package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/modelclient"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) runTurn(ctx context.Context, runtime *SessionServices, modelSession *modelclient.ModelClientSession, turnContext TurnContext, state *TurnState, events protocol.EventSink, canDrainPendingInput bool) (TaskOutput, error) {
	if session == nil || runtime == nil || modelSession == nil || state == nil || events == nil {
		return TaskOutput{}, errors.New("session turn execution is incomplete")
	}
	return session.continueTurn(ctx, runtime, modelSession, turnContext, state, events, canDrainPendingInput)
}
