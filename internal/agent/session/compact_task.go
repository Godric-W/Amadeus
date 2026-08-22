package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func (sessionTask *compactTask) Run(ctx context.Context, session *Session, turnContext *turn.TurnContext) (TaskOutput, error) {
	if sessionTask == nil || sessionTask.runtime == nil || sessionTask.events == nil || session == nil || turnContext == nil {
		return TaskOutput{}, errors.New("compact task is nil")
	}
	modelSession, err := sessionTask.runtime.NewModelClientSession()
	if err != nil {
		return TaskOutput{}, err
	}
	items, err := sessionTask.runtime.Compact(ctx, engine.CompactRequest{
		History: session.ContextProjection(), ModelSession: modelSession,
		Reasoning: llm.ReasoningConfigForEffort(turnContext.ReasoningEffort), Metadata: requestMetadata(*turnContext), Events: sessionTask.events,
	})
	if err != nil {
		return TaskOutput{}, err
	}
	return TaskOutput{Items: items, Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
}
