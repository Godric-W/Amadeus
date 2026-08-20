package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) prepareRegular(_ context.Context, snapshot turn.TurnContext, goal string) (SessionTask, turn.TurnContext, error) {
	if session == nil || session.services.modelClient == nil {
		return nil, turn.TurnContext{}, errors.New("session services are unavailable")
	}
	events, err := protocol.NewScopedSink(session, snapshot.SubmissionID, protocol.ThreadID(snapshot.ThreadID), protocol.TurnID(snapshot.TurnID))
	if err != nil {
		return nil, turn.TurnContext{}, err
	}
	instructionScope, err := newTargetInstructionScope(session.ContextUpdate, session.AppendItems, session.services.instructions, snapshot.TurnID)
	if err != nil {
		return nil, turn.TurnContext{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, turn.TurnContext{}, err
	}
	return &regularTask{runtime: &session.services, goal: goal, events: events, instructions: instructionScope}, snapshot, nil
}

func (sessionTask *regularTask) run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (TaskOutput, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.instructions == nil || sessionTask.events == nil {
		return TaskOutput{}, errors.New("regular task is nil")
	}
	for _, warning := range sessionTask.runtime.SkillWarnings() {
		if warning != nil {
			if err := sessionTask.events.Publish(ctx, protocol.Event{Msg: protocol.WarningEvent{Message: warning.Error()}}); err != nil {
				return TaskOutput{}, err
			}
		}
	}
	if err := sessionTask.runtime.PrepareTurn(ctx, sessionTask.goal, turnContext, sessionTask.instructions, session.ContextUpdate, session.AppendItems); err != nil {
		return TaskOutput{}, err
	}
	output, err := session.runTurn(ctx, sessionTask.runtime, *turnContext, sessionTask.events, sessionTask.instructions)
	if err != nil {
		if ctx.Err() != nil {
			output.Summary = "result: cancelled"
			output.Outcome = protocol.TurnOutcomeAborted
			output.Reason = context.Cause(ctx).Error()
			return output, err
		}
		output.Outcome = protocol.TurnOutcomeFailed
		output.Reason = err.Error()
		return output, err
	}
	if !output.Outcome.Valid() {
		output.Outcome = protocol.TurnOutcomeCompleted
	}
	return output, nil
}
