package session

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/contextmanager"
)

func (session *Session) captureStep(ctx context.Context, services *SessionServices, turnContext TurnContext) (StepContext, error) {
	step, err := services.CaptureStep(ctx, turnContext)
	if err != nil {
		return StepContext{}, err
	}
	if err := session.syncWorldState(ctx, services, step); err != nil {
		return StepContext{}, err
	}
	return step, nil
}

func (session *Session) WorldStateBaseline() (contextmanager.WorldStateSnapshot, bool) {
	if session == nil || session.state.Context == nil {
		return nil, false
	}
	return session.state.Context.WorldStateBaseline()
}
