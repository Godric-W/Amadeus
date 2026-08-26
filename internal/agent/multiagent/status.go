package multiagent

import (
	"context"
	"strings"
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
	if completed, ok := message.(protocol.ItemCompletedEvent); ok && completed.Item.Kind == protocol.ItemAssistantMessage {
		agent.latestAssistant = boundMessage(completed.Item.Text)
		control.mu.Unlock()
		return
	}
	if _, started := message.(protocol.TurnStartedEvent); started {
		agent.latestAssistant = ""
	}
	status, changed := statusFromEvent(message, agent.latestAssistant)
	if !changed || status == agent.status {
		control.mu.Unlock()
		return
	}
	agent.status = status
	if status.Kind == protocol.AgentStatusRunning {
		agent.notifiedTurn = false
	}
	var notification *Notification
	shouldNotify := false
	switch status.Kind {
	case protocol.AgentStatusCompleted, protocol.AgentStatusErrored:
		shouldNotify = !agent.notifiedTurn
		agent.notifiedTurn = true
	case protocol.AgentStatusShutdown:
		shouldNotify = !agent.notifiedShutdown
		agent.notifiedShutdown = true
	}
	if shouldNotify {
		value := Notification{Metadata: agent.metadata, Status: status}
		notification = &value
	}
	control.signalLocked()
	control.mu.Unlock()
	if notification != nil {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = control.host.NotifyParent(notifyCtx, notification.Metadata.ParentThreadID, *notification)
		cancel()
	}
}

func statusFromEvent(message protocol.EventMsg, latestAssistant string) (protocol.AgentStatus, bool) {
	switch event := message.(type) {
	case protocol.TurnStartedEvent:
		return protocol.AgentStatus{Kind: protocol.AgentStatusRunning}, true
	case protocol.TurnCompleteEvent:
		if event.Status == protocol.TurnStatusFailed || strings.TrimSpace(event.Error) != "" {
			text := event.Error
			if strings.TrimSpace(text) == "" {
				text = event.Reason
			}
			return protocol.AgentStatus{Kind: protocol.AgentStatusErrored, Message: boundMessage(text)}, true
		}
		text := latestAssistant
		if strings.TrimSpace(text) == "" {
			text = event.Summary
		}
		return protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, Message: boundMessage(text)}, true
	case protocol.TurnAbortedEvent:
		return protocol.AgentStatus{Kind: protocol.AgentStatusInterrupted}, true
	case protocol.ErrorEvent:
		return protocol.AgentStatus{Kind: protocol.AgentStatusErrored, Message: boundMessage(event.Message)}, true
	case protocol.ShutdownCompleteEvent:
		return protocol.AgentStatus{Kind: protocol.AgentStatusShutdown}, true
	default:
		return protocol.AgentStatus{}, false
	}
}

func (control *Control) waitSnapshot(ids []protocol.ThreadID) ([]StatusSnapshot, bool, <-chan struct{}) {
	control.mu.Lock()
	defer control.mu.Unlock()
	result := make([]StatusSnapshot, 0, len(ids))
	ready := true
	for _, id := range ids {
		snapshot := control.snapshotLocked(id)
		result = append(result, snapshot)
		if snapshot.Status.IsRunning() {
			ready = false
		}
	}
	return result, ready, control.changed
}

func (control *Control) snapshots(ids []protocol.ThreadID) []StatusSnapshot {
	control.mu.Lock()
	defer control.mu.Unlock()
	result := make([]StatusSnapshot, 0, len(ids))
	for _, id := range ids {
		result = append(result, control.snapshotLocked(id))
	}
	return result
}

func (control *Control) snapshotLocked(id protocol.ThreadID) StatusSnapshot {
	agent := control.agents[id]
	if agent == nil {
		return notFoundSnapshot(id)
	}
	return StatusSnapshot{
		AgentID: id, Nickname: agent.metadata.AgentNickname, Role: agent.metadata.AgentRole, Status: agent.status,
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

func boundMessage(message string) string {
	value := []rune(strings.TrimSpace(message))
	if len(value) > maxStatusMessageRunes {
		value = value[:maxStatusMessageRunes]
	}
	return string(value)
}
