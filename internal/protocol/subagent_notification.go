package protocol

import (
	"errors"
	"strings"
)

type SubagentNotificationEvent struct {
	ThreadID ThreadID `json:"thread_id"`
	AgentID  ThreadID `json:"agent_id"`
	TurnID   TurnID   `json:"turn_id"`
	Content  string   `json:"content"`
}

func (SubagentNotificationEvent) isEventMsg() {}

func (event SubagentNotificationEvent) ValidatePayload() error {
	if event.AgentID.IsZero() || strings.TrimSpace(string(event.TurnID)) == "" {
		return errors.New("subagent notification identity is incomplete")
	}
	if strings.TrimSpace(event.Content) == "" {
		return errors.New("subagent notification content is empty")
	}
	return nil
}
