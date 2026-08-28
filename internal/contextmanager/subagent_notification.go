package contextmanager

import (
	"encoding/json"
	"errors"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

type SubagentNotification struct {
	AgentID  protocol.ThreadID
	Nickname string
	Status   protocol.AgentStatus
	LastTurn *protocol.AgentTurnResult
}

func (notification SubagentNotification) Fragment() (ContextFragment, error) {
	if notification.AgentID.IsZero() || notification.LastTurn == nil {
		return ContextFragment{}, errors.New("subagent notification is incomplete")
	}
	if err := notification.Status.Validate(); err != nil {
		return ContextFragment{}, err
	}
	if err := notification.LastTurn.Validate(); err != nil {
		return ContextFragment{}, err
	}
	status := map[string]string{string(notification.Status.Kind): notification.Status.Message}
	payload, err := json.Marshal(struct {
		AgentID    protocol.ThreadID         `json:"agent_id"`
		Nickname   string                    `json:"nickname"`
		Status     map[string]string         `json:"status"`
		TurnResult *protocol.AgentTurnResult `json:"turn_result"`
	}{
		AgentID: notification.AgentID, Nickname: notification.Nickname,
		Status: status, TurnResult: notification.LastTurn,
	})
	if err != nil {
		return ContextFragment{}, err
	}
	return ContextFragment{
		Kind: "multi_agent.subagent_notification", Role: llm.RoleUser,
		Content: "<subagent_notification>\n" + string(payload) + "\n</subagent_notification>", Separate: true,
	}, nil
}
