package llm

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/protocol/identity"
)

type RequestMetadata struct {
	SessionID      identity.SessionID
	ThreadID       identity.ThreadID
	TurnID         identity.TurnID
	ParentThreadID *identity.ThreadID
}

func (metadata RequestMetadata) Validate() error {
	if metadata.SessionID.IsZero() || metadata.ThreadID.IsZero() || metadata.TurnID == "" {
		return errors.New("model request identity metadata is incomplete")
	}
	if metadata.ParentThreadID != nil {
		if metadata.ParentThreadID.IsZero() || *metadata.ParentThreadID == metadata.ThreadID {
			return errors.New("model request parent thread identity is invalid")
		}
	}
	return nil
}

func (metadata RequestMetadata) Values() map[string]string {
	if metadata.Validate() != nil {
		return nil
	}
	values := map[string]string{
		"session_id": metadata.SessionID.String(),
		"thread_id":  metadata.ThreadID.String(),
		"turn_id":    string(metadata.TurnID),
	}
	if metadata.ParentThreadID != nil {
		values["parent_thread_id"] = metadata.ParentThreadID.String()
	}
	return values
}
