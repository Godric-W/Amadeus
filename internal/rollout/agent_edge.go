package rollout

import (
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type AgentSpawnEdgeItem struct {
	AgentID        protocol.ThreadID            `json:"agent_id"`
	ParentThreadID protocol.ThreadID            `json:"parent_thread_id"`
	State          protocol.AgentSpawnEdgeState `json:"state"`
	UpdatedAt      time.Time                    `json:"updated_at"`
}

func (AgentSpawnEdgeItem) isRolloutItem() {}

func (item AgentSpawnEdgeItem) Validate() error {
	if item.AgentID.IsZero() || item.ParentThreadID.IsZero() {
		return errors.New("agent spawn edge identity is incomplete")
	}
	if item.AgentID == item.ParentThreadID {
		return errors.New("agent spawn edge cannot reference itself")
	}
	if !item.State.Valid() {
		return errors.New("agent spawn edge state is invalid")
	}
	if item.UpdatedAt.IsZero() {
		return errors.New("agent spawn edge timestamp is zero")
	}
	return nil
}
