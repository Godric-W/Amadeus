package threadmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (manager *ThreadManager) SpawnChild(ctx context.Context, control *multiagent.Control, request multiagent.SpawnChildRequest) (multiagent.AgentRuntime, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	if control == nil || control.SessionID().IsZero() || control.RootThreadID().IsZero() {
		return nil, errors.New("child agent control is unavailable")
	}
	manager.mu.Lock()
	parent := manager.threads[request.ParentThreadID]
	manager.mu.Unlock()
	if parent == nil || parent.agentControl != control {
		return nil, fmt.Errorf("parent thread %q is unavailable", request.ParentThreadID)
	}
	id, err := protocol.NewThreadID()
	if err != nil {
		return nil, err
	}
	configuration := parent.session.Configuration()
	configuration.Source = protocol.NewSubAgentSessionSource(request.ParentThreadID, request.Depth, request.Nickname, request.Role)
	configuration.Mode = protocol.ModeKindDefault
	live, err := threadstore.NewDraftLiveThread(id, manager.store)
	if err != nil {
		return nil, err
	}
	parentID := request.ParentThreadID
	child, err := manager.spawn(ctx, control.SessionID(), id, &parentID, live, threadstore.InitialHistory{Kind: threadstore.InitialHistoryNew}, StartInput{Configuration: configuration}, control, false)
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	return child, nil
}

func (manager *ThreadManager) NotifyParent(ctx context.Context, parentID protocol.ThreadID, notification multiagent.Notification) error {
	manager.mu.Lock()
	parent := manager.threads[parentID]
	manager.mu.Unlock()
	if parent == nil || parent.session == nil {
		return fmt.Errorf("parent thread %q is unavailable", parentID)
	}
	if notification.LastTurn == nil || notification.LastTurn.TurnID == "" {
		return errors.New("subagent notification has no terminal turn")
	}
	fragment, err := (contextmanager.SubagentNotification{
		AgentID: notification.Metadata.ThreadID, Nickname: notification.Metadata.AgentNickname,
		Status: notification.Status, LastTurn: notification.LastTurn,
	}).Fragment()
	if err != nil {
		return err
	}
	return parent.session.AppendSubagentNotification(ctx, notification.Metadata.ThreadID, notification.LastTurn.TurnID, fragment)
}

func (manager *ThreadManager) RecordSpawnEdge(ctx context.Context, parentID, agentID protocol.ThreadID, state protocol.AgentSpawnEdgeState) error {
	manager.mu.Lock()
	parent := manager.threads[parentID]
	manager.mu.Unlock()
	if parent == nil || parent.session == nil {
		return fmt.Errorf("parent thread %q is unavailable", parentID)
	}
	return parent.session.RecordAgentSpawnEdge(ctx, agentID, state, manager.services.Clock().UTC())
}
