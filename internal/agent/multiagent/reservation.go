package multiagent

import (
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func (control *Control) childDepth(parentID protocol.ThreadID) (int, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.closed {
		return 0, errors.New("agent control is closed")
	}
	if parentID == control.rootID {
		return 1, nil
	}
	parent := control.agents[parentID]
	if parent == nil {
		return 0, fmt.Errorf("parent agent %q was not found", parentID)
	}
	depth := parent.metadata.Depth + 1
	if depth > control.maxDepth {
		return 0, fmt.Errorf("agent depth %d exceeds configured maximum %d", depth, control.maxDepth)
	}
	return depth, nil
}

func (control *Control) reserve() (string, string, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.closed {
		return "", "", errors.New("agent control is closed")
	}
	if len(control.agents)+len(control.reservations) >= control.maxAgents {
		return "", "", fmt.Errorf("agent limit %d reached", control.maxAgents)
	}
	nickname := ""
	for _, candidate := range agentNicknames {
		if _, used := control.nicknames[candidate]; !used {
			nickname = candidate
			break
		}
	}
	if nickname == "" {
		return "", "", errors.New("agent nickname pool is exhausted")
	}
	reservationID := nickname
	control.nicknames[nickname] = struct{}{}
	control.reservations[reservationID] = reservation{nickname: nickname}
	return reservationID, nickname, nil
}

func (control *Control) rollbackReservation(id string) {
	control.mu.Lock()
	defer control.mu.Unlock()
	reservation, exists := control.reservations[id]
	if !exists {
		return
	}
	delete(control.reservations, id)
	delete(control.nicknames, reservation.nickname)
	control.signalLocked()
}

func (control *Control) commitReservation(id string, metadata protocol.AgentMetadata, runtime AgentRuntime) error {
	control.mu.Lock()
	defer control.mu.Unlock()
	reservation, exists := control.reservations[id]
	if !exists || reservation.nickname != metadata.AgentNickname {
		return errors.New("agent reservation was lost")
	}
	if control.closed {
		return errors.New("agent control closed during spawn")
	}
	if _, exists := control.agents[metadata.ThreadID]; exists {
		return fmt.Errorf("agent %q already exists", metadata.ThreadID)
	}
	delete(control.reservations, id)
	control.agents[metadata.ThreadID] = &record{
		metadata: metadata, status: protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit}, runtime: runtime,
	}
	control.signalLocked()
	return nil
}

func (control *Control) runtimeForInput(id protocol.ThreadID) (AgentRuntime, protocol.AgentStatus, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	agent := control.agents[id]
	if agent == nil || agent.closing {
		return nil, protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}, fmt.Errorf("agent %q is not available", id)
	}
	switch agent.status.Kind {
	case protocol.AgentStatusShutdown, protocol.AgentStatusNotFound:
		return nil, agent.status, fmt.Errorf("agent %q is not available", id)
	default:
		return agent.runtime, agent.status, nil
	}
}
