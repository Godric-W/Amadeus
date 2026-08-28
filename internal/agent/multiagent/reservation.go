package multiagent

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/protocol"
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

func (control *Control) installReservation(id string, metadata protocol.AgentMetadata, runtime AgentRuntime) error {
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
		metadata: metadata, status: protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit}, runtime: runtime, provisional: true,
	}
	control.signalLocked()
	return nil
}

func (control *Control) commitInstalledAgent(id protocol.ThreadID) (*Notification, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	agent := control.agents[id]
	if agent == nil || !agent.provisional || agent.closing {
		return nil, fmt.Errorf("installed agent %q is unavailable", id)
	}
	agent.provisional = false
	notification := pendingNotificationLocked(agent)
	control.signalLocked()
	return notification, nil
}

func (control *Control) RegisterPersisted(metadata protocol.AgentMetadata, state LifecycleState, notifiedTurnID protocol.TurnID) error {
	if control == nil {
		return errors.New("agent control is nil")
	}
	if err := metadata.Validate(); err != nil {
		return err
	}
	if err := state.Status.Validate(); err != nil {
		return err
	}
	if state.LastTurn != nil {
		if err := state.LastTurn.Validate(); err != nil {
			return err
		}
	}
	if state.Status.Kind == protocol.AgentStatusRunning {
		return errors.New("persisted agent cannot have running status")
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.closed {
		return errors.New("agent control is closed")
	}
	if len(control.agents)+len(control.reservations) >= control.maxAgents {
		return fmt.Errorf("agent limit %d reached while restoring persisted agents", control.maxAgents)
	}
	if _, exists := control.agents[metadata.ThreadID]; exists {
		return fmt.Errorf("agent %q already exists", metadata.ThreadID)
	}
	if _, used := control.nicknames[metadata.AgentNickname]; used {
		return fmt.Errorf("agent nickname %q is already in use", metadata.AgentNickname)
	}
	control.nicknames[metadata.AgentNickname] = struct{}{}
	control.agents[metadata.ThreadID] = &record{metadata: metadata, status: state.Status, lastTurn: cloneTurnResult(state.LastTurn), notifiedTurnID: notifiedTurnID}
	control.signalLocked()
	return nil
}

func (control *Control) runtimeForInput(ctx context.Context, id protocol.ThreadID) (AgentRuntime, protocol.AgentStatus, error) {
	control.resumeMu.Lock()
	defer control.resumeMu.Unlock()
	control.mu.Lock()
	agent := control.agents[id]
	if agent == nil || agent.closing {
		control.mu.Unlock()
		return nil, protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}, fmt.Errorf("agent %q is not available", id)
	}
	switch agent.status.Kind {
	case protocol.AgentStatusShutdown, protocol.AgentStatusNotFound:
		control.mu.Unlock()
		return nil, agent.status, fmt.Errorf("agent %q is not available", id)
	}
	if agent.runtime != nil {
		runtime, status := agent.runtime, agent.status
		control.mu.Unlock()
		return runtime, status, nil
	}
	control.mu.Unlock()
	runtime, err := control.host.ResumeChild(ctx, control, id)
	if err != nil {
		return nil, protocol.AgentStatus{}, fmt.Errorf("resume agent %q: %w", id, err)
	}
	if runtime == nil || runtime.ID() != id {
		if runtime != nil {
			_ = shutdownRuntime(ctx, runtime)
		}
		return nil, protocol.AgentStatus{}, fmt.Errorf("resume agent %q returned inconsistent runtime", id)
	}
	control.mu.Lock()
	agent = control.agents[id]
	if agent == nil || agent.closing || agent.runtime != nil {
		control.mu.Unlock()
		_ = shutdownRuntime(ctx, runtime)
		return nil, protocol.AgentStatus{}, fmt.Errorf("agent %q changed while resuming", id)
	}
	agent.runtime = runtime
	status := agent.status
	control.mu.Unlock()
	go control.consume(id, runtime)
	return runtime, status, nil
}
