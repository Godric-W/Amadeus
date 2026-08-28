package protocol

import (
	"errors"
	"fmt"
	"strings"
)

type SessionSourceKind string

const (
	SessionSourceRoot     SessionSourceKind = "root"
	SessionSourceSubAgent SessionSourceKind = "subagent"
)

type SubAgentSource struct {
	ParentThreadID ThreadID `json:"parent_thread_id"`
	Depth          int      `json:"depth"`
	AgentNickname  string   `json:"agent_nickname"`
	AgentRole      string   `json:"agent_role"`
}

type SessionSource struct {
	Kind     SessionSourceKind `json:"kind"`
	SubAgent *SubAgentSource   `json:"subagent,omitempty"`
}

func RootSessionSource() SessionSource {
	return SessionSource{Kind: SessionSourceRoot}
}

func NewSubAgentSessionSource(parentThreadID ThreadID, depth int, nickname, role string) SessionSource {
	return SessionSource{
		Kind: SessionSourceSubAgent,
		SubAgent: &SubAgentSource{
			ParentThreadID: parentThreadID,
			Depth:          depth,
			AgentNickname:  strings.TrimSpace(nickname),
			AgentRole:      strings.TrimSpace(role),
		},
	}
}

func (source SessionSource) Validate() error {
	switch source.Kind {
	case SessionSourceRoot:
		if source.SubAgent != nil {
			return errors.New("root session source has sub-agent metadata")
		}
		return nil
	case SessionSourceSubAgent:
		if source.SubAgent == nil {
			return errors.New("sub-agent session source metadata is missing")
		}
		if source.SubAgent.ParentThreadID.IsZero() {
			return errors.New("sub-agent parent thread ID is empty")
		}
		if source.SubAgent.Depth <= 0 {
			return errors.New("sub-agent depth must be positive")
		}
		if strings.TrimSpace(source.SubAgent.AgentNickname) == "" || strings.TrimSpace(source.SubAgent.AgentRole) == "" {
			return errors.New("sub-agent identity is incomplete")
		}
		return nil
	default:
		return fmt.Errorf("session source kind %q is invalid", source.Kind)
	}
}

func (source SessionSource) IsSubAgent() bool {
	return source.Kind == SessionSourceSubAgent && source.SubAgent != nil
}

func (source SessionSource) Clone() SessionSource {
	if source.SubAgent == nil {
		return source
	}
	cloned := *source.SubAgent
	source.SubAgent = &cloned
	return source
}

type AgentStatusKind string

const (
	AgentStatusPendingInit AgentStatusKind = "pending_init"
	AgentStatusRunning     AgentStatusKind = "running"
	AgentStatusInterrupted AgentStatusKind = "interrupted"
	AgentStatusCompleted   AgentStatusKind = "completed"
	AgentStatusErrored     AgentStatusKind = "errored"
	AgentStatusShutdown    AgentStatusKind = "shutdown"
	AgentStatusNotFound    AgentStatusKind = "not_found"
)

type AgentStatus struct {
	Kind    AgentStatusKind `json:"kind"`
	Message string          `json:"message,omitempty"`
}

func (status AgentStatus) Validate() error {
	switch status.Kind {
	case AgentStatusCompleted, AgentStatusErrored:
		return nil
	case AgentStatusPendingInit, AgentStatusRunning, AgentStatusInterrupted, AgentStatusShutdown, AgentStatusNotFound:
		if strings.TrimSpace(status.Message) != "" {
			return fmt.Errorf("agent status %q must not carry a message", status.Kind)
		}
		return nil
	default:
		return fmt.Errorf("agent status kind %q is invalid", status.Kind)
	}
}

func (status AgentStatus) IsRunning() bool {
	return status.Kind == AgentStatusPendingInit || status.Kind == AgentStatusRunning
}

func (status AgentStatus) IsFinal() bool {
	switch status.Kind {
	case AgentStatusCompleted, AgentStatusErrored, AgentStatusShutdown, AgentStatusNotFound:
		return true
	default:
		return false
	}
}

type AgentTurnResult struct {
	TurnID           TurnID      `json:"turn_id"`
	Outcome          TurnOutcome `json:"outcome"`
	Reason           string      `json:"reason,omitempty"`
	LastAgentMessage *string     `json:"last_agent_message,omitempty"`
}

func (result AgentTurnResult) Validate() error {
	if strings.TrimSpace(string(result.TurnID)) == "" {
		return errors.New("agent turn result turn ID is empty")
	}
	if !result.Outcome.Valid() {
		return fmt.Errorf("agent turn result outcome %q is invalid", result.Outcome)
	}
	if result.LastAgentMessage != nil && strings.TrimSpace(*result.LastAgentMessage) == "" {
		return errors.New("agent turn result last message is empty")
	}
	return nil
}

func (result AgentTurnResult) Clone() AgentTurnResult {
	if result.LastAgentMessage != nil {
		message := *result.LastAgentMessage
		result.LastAgentMessage = &message
	}
	return result
}

type AgentMetadata struct {
	ThreadID       ThreadID `json:"thread_id"`
	ParentThreadID ThreadID `json:"parent_thread_id"`
	Depth          int      `json:"depth"`
	AgentNickname  string   `json:"agent_nickname"`
	AgentRole      string   `json:"agent_role"`
}

type AgentSpawnEdgeState string

const (
	AgentSpawnEdgeOpen   AgentSpawnEdgeState = "open"
	AgentSpawnEdgeClosed AgentSpawnEdgeState = "closed"
)

func (state AgentSpawnEdgeState) Valid() bool {
	return state == AgentSpawnEdgeOpen || state == AgentSpawnEdgeClosed
}

func (metadata AgentMetadata) Validate() error {
	if metadata.ThreadID.IsZero() || metadata.ParentThreadID.IsZero() {
		return errors.New("agent thread identity is incomplete")
	}
	if metadata.ThreadID == metadata.ParentThreadID {
		return errors.New("agent thread cannot be its own parent")
	}
	if metadata.Depth <= 0 {
		return errors.New("agent depth must be positive")
	}
	if strings.TrimSpace(metadata.AgentNickname) == "" || strings.TrimSpace(metadata.AgentRole) == "" {
		return errors.New("agent metadata is incomplete")
	}
	return nil
}
