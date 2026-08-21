package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (session *Session) prepareRegular(_ context.Context, snapshot turn.TurnContext, goal string, state *TurnState) (SessionTask, turn.TurnContext, error) {
	if session == nil || session.services.modelClient == nil {
		return nil, turn.TurnContext{}, errors.New("session services are unavailable")
	}
	events, err := protocol.NewScopedSink(session, snapshot.SubmissionID, protocol.ThreadID(snapshot.ThreadID), protocol.TurnID(snapshot.TurnID))
	if err != nil {
		return nil, turn.TurnContext{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, turn.TurnContext{}, err
	}
	if state == nil {
		return nil, turn.TurnContext{}, errors.New("regular task turn state is nil")
	}
	return &regularTask{runtime: &session.services, goal: goal, events: events, turnState: state}, snapshot, nil
}

func (sessionTask *regularTask) run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (TaskOutput, error) {
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
		if err := session.prepareTurn(ctx, sessionTask.runtime, sessionTask.goal, turnContext); err != nil {
			return TaskOutput{}, err
		}
		modelSession, err := sessionTask.runtime.NewModelClientSession()
		if err != nil {
			return TaskOutput{}, err
		}
		sessionTask.modelSession = modelSession
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
