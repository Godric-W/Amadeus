package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) prepareRegular(_ context.Context, snapshot TurnContext, initialInput TurnInput, state *TurnState) (SessionTask, TurnContext, error) {
	if session == nil || session.services.modelClient == nil {
		return nil, TurnContext{}, errors.New("session services are unavailable")
	}
	events, err := protocol.NewScopedSink(session, snapshot.SubmissionID, snapshot.ThreadID, snapshot.TurnID)
	if err != nil {
		return nil, TurnContext{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, TurnContext{}, err
	}
	if state == nil {
		return nil, TurnContext{}, errors.New("regular task turn state is nil")
	}
	if initialInput == nil {
		return nil, TurnContext{}, errors.New("regular task initial input is nil")
	}
	return &regularTask{runtime: &session.services, initialInput: initialInput, events: events, turnState: state}, snapshot, nil
}

func (sessionTask *regularTask) run(ctx context.Context, session *Session, turnContext *TurnContext) (TaskOutput, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.events == nil {
		return TaskOutput{}, errors.New("regular task is nil")
	}
	firstRun := !sessionTask.initialized
	if firstRun {
		for _, warning := range sessionTask.runtime.SkillWarnings() {
			if warning != nil {
				if err := sessionTask.events.Publish(ctx, protocol.Event{Msg: protocol.WarningEvent{Message: warning.Error()}}); err != nil {
					return TaskOutput{}, err
				}
			}
		}
		modelSession, err := sessionTask.runtime.NewModelClientSession()
		if err != nil {
			return TaskOutput{}, err
		}
		sessionTask.modelSession = modelSession
		if err := session.prepareInitialTurnInput(ctx, sessionTask, *turnContext); err != nil {
			return TaskOutput{}, err
		}
		sessionTask.initialized = true
	}
	if sessionTask.modelSession == nil {
		return TaskOutput{}, errors.New("regular task model session is nil")
	}
	output, err := session.runTurn(ctx, sessionTask.runtime, sessionTask.modelSession, *turnContext, sessionTask.turnState, sessionTask.events, !firstRun)
	if err != nil {
		return output, err
	}
	if !output.Outcome.Valid() {
		output.Outcome = protocol.TurnOutcomeCompleted
	}
	return output, nil
}
