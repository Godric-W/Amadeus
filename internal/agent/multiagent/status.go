package multiagent

import (
	"context"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (control *Control) consume(id protocol.ThreadID, runtime AgentRuntime) {
	for {
		select {
		case event, ok := <-runtime.Events():
			if !ok {
				select {
				case <-runtime.Terminated():
					control.reduceEvent(id, protocol.ShutdownCompleteEvent{ThreadID: id})
				case <-control.done:
				}
				return
			}
			control.reduceEvent(id, event.Msg)
		case <-control.done:
			return
		}
	}
}

func (control *Control) reduceEvent(id protocol.ThreadID, message protocol.EventMsg) {
	control.mu.Lock()
	agent := control.agents[id]
	if agent == nil {
		control.mu.Unlock()
		return
	}
	current := LifecycleState{Status: agent.status, LastTurn: agent.lastTurn}
	next, changed, terminal := ReduceLifecycleEvent(current, message)
	if !changed {
		control.mu.Unlock()
		return
	}
	agent.status = next.Status
	agent.lastTurn = cloneTurnResult(next.LastTurn)
	var notification *Notification
	if terminal {
		notification = pendingNotificationLocked(agent)
	}
	control.signalLocked()
	control.mu.Unlock()
	control.deliverNotification(notification)
}

func pendingNotificationLocked(agent *record) *Notification {
	if agent == nil || agent.provisional || agent.lastTurn == nil || agent.lastTurn.TurnID == "" || !agent.status.IsFinal() || agent.status.Kind != protocol.AgentStatusCompleted && agent.status.Kind != protocol.AgentStatusErrored {
		return nil
	}
	turnID := agent.lastTurn.TurnID
	if agent.notifiedTurnID == turnID || agent.notifyingTurnID == turnID {
		return nil
	}
	agent.notifyingTurnID = turnID
	return &Notification{Metadata: agent.metadata, Status: agent.status, LastTurn: cloneTurnResult(agent.lastTurn)}
}

func (control *Control) deliverNotification(notification *Notification) {
	if control == nil || notification == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := control.host.NotifyParent(notifyCtx, notification.Metadata.ParentThreadID, *notification)
	cancel()
	control.mu.Lock()
	agent := control.agents[notification.Metadata.ThreadID]
	if agent != nil && notification.LastTurn != nil && agent.notifyingTurnID == notification.LastTurn.TurnID {
		agent.notifyingTurnID = ""
		if err == nil {
			agent.notifiedTurnID = notification.LastTurn.TurnID
			agent.notificationError = ""
		} else {
			agent.notificationError = boundMessage(err.Error())
		}
		control.signalLocked()
	}
	control.mu.Unlock()
}

func (control *Control) waitSnapshot(ids []protocol.ThreadID) ([]StatusSnapshot, bool, <-chan struct{}) {
	control.mu.Lock()
	defer control.mu.Unlock()
	result := make([]StatusSnapshot, 0, len(ids))
	ready := false
	for _, id := range ids {
		snapshot := control.snapshotLocked(id)
		if isFinalSnapshot(snapshot) {
			result = append(result, snapshot)
			ready = true
		}
	}
	return result, ready, control.changed
}

func isFinalSnapshot(snapshot StatusSnapshot) bool {
	switch snapshot.Status.Kind {
	case protocol.AgentStatusCompleted, protocol.AgentStatusErrored:
		return snapshot.LastTurn != nil
	case protocol.AgentStatusShutdown, protocol.AgentStatusNotFound:
		return true
	default:
		return false
	}
}

func (control *Control) snapshotLocked(id protocol.ThreadID) StatusSnapshot {
	agent := control.agents[id]
	if agent == nil {
		return notFoundSnapshot(id)
	}
	return StatusSnapshot{
		AgentID: id, Nickname: agent.metadata.AgentNickname, Role: agent.metadata.AgentRole,
		Status: agent.status, LastTurn: cloneTurnResult(agent.lastTurn), NotificationError: agent.notificationError,
	}
}

func (control *Control) signalLocked() {
	close(control.changed)
	control.changed = make(chan struct{})
}

func notFoundSnapshot(id protocol.ThreadID) StatusSnapshot {
	return StatusSnapshot{AgentID: id, Status: protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}}
}

func uniqueIDs(ids []protocol.ThreadID) []protocol.ThreadID {
	result := make([]protocol.ThreadID, 0, len(ids))
	seen := make(map[protocol.ThreadID]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func containsID(ids []protocol.ThreadID, id protocol.ThreadID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func cloneTurnResult(value *protocol.AgentTurnResult) *protocol.AgentTurnResult {
	if value == nil {
		return nil
	}
	cloned := value.Clone()
	return &cloned
}
