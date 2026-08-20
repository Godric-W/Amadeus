package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (builder *ServicesBuilder) prepareRegular(_ context.Context, session *Session, snapshot turn.TurnContext, goal string) (SessionTask, turn.TurnContext, error) {
	runtime := session.AgentServices()
	if runtime == nil {
		return nil, turn.TurnContext{}, errors.New("session agent services are unavailable")
	}
	events, err := protocol.NewScopedSink(session, snapshot.SubmissionID, protocol.ThreadID(snapshot.ThreadID), protocol.TurnID(snapshot.TurnID))
	if err != nil {
		return nil, turn.TurnContext{}, err
	}
	instructionScope, err := newTargetInstructionScope(session.ContextUpdate, session.AppendItems, builder.instructions, snapshot.TurnID)
	if err != nil {
		return nil, turn.TurnContext{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, turn.TurnContext{}, err
	}
	return &regularTask{runtime: runtime, goal: goal, events: events, instructions: instructionScope}, snapshot, nil
}

func (sessionTask *regularTask) run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (Result, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.instructions == nil || sessionTask.events == nil {
		return Result{}, errors.New("regular task is nil")
	}
	for _, warning := range sessionTask.runtime.SkillWarnings() {
		if warning != nil {
			if err := sessionTask.events.Publish(ctx, protocol.Event{Msg: protocol.WarningEvent{Message: warning.Error()}}); err != nil {
				return Result{}, err
			}
		}
	}
	if err := sessionTask.runtime.PrepareTurn(ctx, sessionTask.goal, turnContext, sessionTask.instructions, session.ContextUpdate, session.AppendItems); err != nil {
		return Result{}, err
	}
	runResult, err := session.runTurn(ctx, sessionTask.runtime, *turnContext, []TurnInput{{Content: sessionTask.goal}}, sessionTask.events, sessionTask.instructions)
	items := make([]rollout.RolloutItem, 0)
	if err != nil {
		if ctx.Err() != nil {
			return Result{Items: items, Summary: "result: cancelled", Outcome: OutcomeAborted, Reason: context.Cause(ctx).Error(), Usage: runResult.Usage, ToolCallCount: runResult.ToolCallCount}, err
		}
		return Result{Items: items, Summary: runResult.Summary, Outcome: OutcomeFailed, Reason: err.Error(), Usage: runResult.Usage, ToolCallCount: runResult.ToolCallCount}, err
	}
	outcome := runResult.Outcome
	if !outcome.Valid() {
		outcome = OutcomeCompleted
	}
	return Result{Items: items, Summary: runResult.Summary, Outcome: outcome, Reason: runResult.Reason, Usage: runResult.Usage, ToolCallCount: runResult.ToolCallCount}, nil
}
