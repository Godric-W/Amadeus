package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func (manager *ThreadManager) restorePersistedChildren(ctx context.Context, root *AmadeusThread, control *multiagent.Control) error {
	children, err := manager.store.ListChildren(ctx, root.id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if !child.Source.IsSubAgent() || child.Source.SubAgent.ParentThreadID != root.id {
			return fmt.Errorf("persisted child %q has invalid parent metadata", child.ID)
		}
		history, err := manager.store.LoadHistory(ctx, child.ID)
		if err != nil {
			return err
		}
		meta, err := canonicalChildMeta(history, control.SessionID(), child.ID, root.id)
		if err != nil {
			return err
		}
		metadata := protocol.AgentMetadata{
			ThreadID: child.ID, ParentThreadID: root.id, Depth: meta.Source.SubAgent.Depth,
			AgentNickname: meta.Source.SubAgent.AgentNickname, AgentRole: meta.Source.SubAgent.AgentRole,
		}
		if err := control.RegisterPersisted(metadata, persistedAgentStatus(history)); err != nil {
			return err
		}
	}
	return nil
}

func (manager *ThreadManager) ResumeChild(ctx context.Context, control *multiagent.Control, id protocol.ThreadID) (multiagent.AgentRuntime, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	if control == nil || id.IsZero() {
		return nil, errors.New("child resume identity is incomplete")
	}
	if existing, ok := manager.GetThread(id); ok {
		if existing.agentControl != control || existing.sessionID != control.SessionID() {
			return nil, errors.New("loaded child belongs to a different agent control")
		}
		return existing, nil
	}
	record, ok := control.Record(id)
	if !ok {
		return nil, fmt.Errorf("persisted child %q is not registered", id)
	}
	stored, err := manager.store.GetThread(ctx, id)
	if err != nil {
		return nil, err
	}
	if !stored.Source.IsSubAgent() || stored.Source.SubAgent.ParentThreadID != record.Metadata.ParentThreadID {
		return nil, errors.New("persisted child metadata is inconsistent")
	}
	parent, ok := manager.GetThread(record.Metadata.ParentThreadID)
	if !ok || parent.agentControl != control || parent.sessionID != control.SessionID() {
		return nil, errors.New("persisted child parent runtime is unavailable")
	}
	live, history, err := thread.NewResumedLiveThread(ctx, id, manager.store)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = live.Shutdown(context.Background())
		}
	}()
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	history, err = thread.RecoverInterruptedTurn(recoveryCtx, live, history)
	cancel()
	if err != nil {
		return nil, err
	}
	if _, err := canonicalChildMeta(history, control.SessionID(), id, record.Metadata.ParentThreadID); err != nil {
		return nil, err
	}
	configuration := parent.session.Configuration()
	configuration.Source = stored.Source.Clone()
	configuration.Mode = turn.ModeKindDefault
	parentID := record.Metadata.ParentThreadID
	child, err := manager.spawn(ctx, control.SessionID(), id, &parentID, live, history, StartInput{Configuration: configuration}, control, false)
	if err != nil {
		return nil, err
	}
	cleanup = false
	return child, nil
}

func canonicalChildMeta(history thread.InitialHistory, sessionID protocol.SessionID, id, parentID protocol.ThreadID) (rollout.SessionMetaItem, error) {
	if err := history.Validate(id); err != nil {
		return rollout.SessionMetaItem{}, err
	}
	meta, ok := history.Lines[0].Item.(rollout.SessionMetaItem)
	if !ok || !meta.Source.IsSubAgent() {
		return rollout.SessionMetaItem{}, errors.New("child rollout does not begin with sub-agent session metadata")
	}
	if meta.SessionID != sessionID || meta.ID != id || meta.ParentThreadID == nil || *meta.ParentThreadID != parentID || meta.Source.SubAgent.ParentThreadID != parentID {
		return rollout.SessionMetaItem{}, errors.New("child rollout identity is inconsistent")
	}
	return meta, nil
}

func persistedAgentStatus(history thread.InitialHistory) protocol.AgentStatus {
	status := protocol.AgentStatus{Kind: protocol.AgentStatusCompleted}
	for _, line := range history.Lines {
		event, ok := line.Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		switch value := event.Msg.(type) {
		case protocol.TurnCompleteEvent:
			if value.Status == protocol.TurnStatusFailed || value.Error != "" {
				status = protocol.AgentStatus{Kind: protocol.AgentStatusErrored, Message: value.Error}
				if status.Message == "" {
					status.Message = value.Reason
				}
			} else {
				status = protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, Message: value.Summary}
			}
		case protocol.TurnAbortedEvent:
			status = protocol.AgentStatus{Kind: protocol.AgentStatusInterrupted}
		case protocol.ErrorEvent:
			status = protocol.AgentStatus{Kind: protocol.AgentStatusErrored, Message: value.Message}
		}
	}
	return status
}
