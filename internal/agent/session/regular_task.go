package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (session *Session) prepareRegular(_ context.Context, snapshot turn.TurnContext, goal string) (SessionTask, turn.TurnContext, error) {
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
	return &regularTask{runtime: &session.services, goal: goal, events: events}, snapshot, nil
}

func (sessionTask *regularTask) run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (TaskOutput, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.events == nil {
		return TaskOutput{}, errors.New("regular task is nil")
	}
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
	output, err := session.runTurn(ctx, sessionTask.runtime, *turnContext, sessionTask.events)
	if err != nil {
		return output, err
	}
	if !output.Outcome.Valid() {
		output.Outcome = protocol.TurnOutcomeCompleted
	}
	return output, nil
}
