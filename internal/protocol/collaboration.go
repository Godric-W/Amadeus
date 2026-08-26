package protocol

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type CollabAgentTool string

const (
	CollabAgentSpawnAgent CollabAgentTool = "spawn_agent"
	CollabAgentSendInput  CollabAgentTool = "send_input"
	CollabAgentWait       CollabAgentTool = "wait_agent"
	CollabAgentCloseAgent CollabAgentTool = "close_agent"
)

func (tool CollabAgentTool) Valid() bool {
	switch tool {
	case CollabAgentSpawnAgent, CollabAgentSendInput, CollabAgentWait, CollabAgentCloseAgent:
		return true
	default:
		return false
	}
}

type CollabAgentToolCallStatus string

const (
	CollabAgentToolInProgress CollabAgentToolCallStatus = "in_progress"
	CollabAgentToolCompleted  CollabAgentToolCallStatus = "completed"
	CollabAgentToolFailed     CollabAgentToolCallStatus = "failed"
)

type CollabAgentRef struct {
	ThreadID      ThreadID `json:"thread_id"`
	AgentNickname string   `json:"agent_nickname,omitempty"`
	AgentRole     string   `json:"agent_role,omitempty"`
}

type CollabAgentState struct {
	Status AgentStatus `json:"status"`
}

type CollabAgentToolCallItem struct {
	ID             ItemID                        `json:"id"`
	Tool           CollabAgentTool               `json:"tool"`
	Status         CollabAgentToolCallStatus     `json:"status"`
	SenderThreadID ThreadID                      `json:"sender_thread_id"`
	ReceiverAgents []CollabAgentRef              `json:"receiver_agents,omitempty"`
	Prompt         string                        `json:"prompt,omitempty"`
	AgentsStates   map[ThreadID]CollabAgentState `json:"agents_states,omitempty"`
	CreatedAt      time.Time                     `json:"created_at"`
	CompletedAt    *time.Time                    `json:"completed_at,omitempty"`
}

func (item CollabAgentToolCallItem) Validate() error {
	if strings.TrimSpace(string(item.ID)) == "" || !item.Tool.Valid() || item.SenderThreadID.IsZero() || item.CreatedAt.IsZero() {
		return errors.New("collaboration agent tool call item is incomplete")
	}
	switch item.Status {
	case CollabAgentToolInProgress:
		if item.CompletedAt != nil {
			return errors.New("in-progress collaboration item has completion time")
		}
	case CollabAgentToolCompleted, CollabAgentToolFailed:
		if item.CompletedAt == nil || item.CompletedAt.IsZero() {
			return errors.New("terminal collaboration item has no completion time")
		}
	default:
		return fmt.Errorf("collaboration agent tool status %q is invalid", item.Status)
	}
	for _, agent := range item.ReceiverAgents {
		if agent.ThreadID.IsZero() {
			return errors.New("collaboration receiver agent ID is empty")
		}
	}
	for id, state := range item.AgentsStates {
		if id.IsZero() {
			return errors.New("collaboration agent state ID is empty")
		}
		if err := state.Status.Validate(); err != nil {
			return err
		}
	}
	return nil
}
