package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (sessionTask *compactTask) Run(ctx context.Context, session *Session, turnContext *TurnContext) (TaskOutput, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.events == nil || session == nil || turnContext == nil {
		return TaskOutput{}, errors.New("compact task is nil")
	}
	modelSession, err := sessionTask.runtime.NewModelClientSession()
	if err != nil {
		return TaskOutput{}, err
	}
	_, err = session.runCompaction(ctx, sessionTask.runtime, modelSession, *turnContext, nil, nil, sessionTask.events, compactionInvocation{
		Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn,
	})
	if err != nil {
		return TaskOutput{}, err
	}
	return TaskOutput{Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
}
