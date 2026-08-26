package session

import (
	"context"
	"errors"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) createTask(ctx context.Context, input string, snapshot TurnContext, kind TaskKind, state *TurnState) (SessionTask, TurnContext, error) {
	now := session.services.Clock()
	if snapshot.CurrentDate == "" {
		snapshot.CurrentDate = now.Format("2006-01-02")
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = now.Location().String()
	}
	if err := snapshot.Validate(); err != nil {
		return nil, TurnContext{}, err
	}
	switch kind {
	case TaskKindCompact:
		events, err := protocol.NewScopedSink(session, snapshot.SubmissionID, snapshot.ThreadID, snapshot.TurnID)
		if err != nil {
			return nil, TurnContext{}, err
		}
		return &compactTask{runtime: &session.services, events: events}, snapshot, nil
	case TaskKindRegular:
	default:
		return nil, TurnContext{}, errors.New("session task kind is invalid")
	}
	goal := input
	if goal == "" {
		return nil, TurnContext{}, errors.New("regular task goal is empty")
	}
	return session.prepareRegular(ctx, snapshot, goal, state)
}
