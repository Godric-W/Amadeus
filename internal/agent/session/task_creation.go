package session

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
)

func (session *Session) createTask(ctx context.Context, input string, snapshot turn.TurnContext, kind TaskKind, state *TurnState) (SessionTask, turn.TurnContext, error) {
	now := session.services.Clock()
	if snapshot.CurrentDate == "" {
		snapshot.CurrentDate = now.Format("2006-01-02")
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = now.Location().String()
	}
	if err := snapshot.Validate(); err != nil {
		return nil, turn.TurnContext{}, err
	}
	switch kind {
	case TaskKindCompact:
		events, err := protocol.NewScopedSink(session, snapshot.SubmissionID, snapshot.ThreadID, snapshot.TurnID)
		if err != nil {
			return nil, turn.TurnContext{}, err
		}
		return &compactTask{runtime: &session.services, events: events}, snapshot, nil
	case TaskKindRegular:
	default:
		return nil, turn.TurnContext{}, errors.New("session task kind is invalid")
	}
	goal := strings.TrimSpace(input)
	if goal == "" {
		return nil, turn.TurnContext{}, errors.New("regular task goal is empty")
	}
	return session.prepareRegular(ctx, snapshot, goal, state)
}
