package threadmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (manager *ThreadManager) restorePersistedChildren(ctx context.Context, root *AmadeusThread, control *multiagent.Control) error {
	lines, err := root.History(ctx)
	if err != nil {
		return err
	}
	openChildren, err := openChildIDs(root.id, lines)
	if err != nil {
		return err
	}
	notifiedTurns := deliveredNotificationTurns(lines)
	for _, childID := range openChildren {
		child, err := manager.store.GetThread(ctx, childID)
		if errors.Is(err, threadstore.ErrNotFound) {
			if rebuildErr := manager.store.RebuildIndex(ctx); rebuildErr != nil {
				return errors.Join(err, rebuildErr)
			}
			child, err = manager.store.GetThread(ctx, childID)
		}
		if err != nil {
			return err
		}
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
		if err := control.RegisterPersisted(metadata, persistedAgentLifecycle(history), notifiedTurns[child.ID]); err != nil {
			return err
		}
	}
	return nil
}

func deliveredNotificationTurns(lines []rollout.Line) map[protocol.ThreadID]protocol.TurnID {
	result := make(map[protocol.ThreadID]protocol.TurnID)
	for _, line := range lines {
		event, ok := line.Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		notification, ok := event.Msg.(protocol.SubagentNotificationEvent)
		if !ok || notification.AgentID.IsZero() || notification.TurnID == "" {
			continue
		}
		result[notification.AgentID] = notification.TurnID
	}
	return result
}

func openChildIDs(rootID protocol.ThreadID, lines []rollout.Line) ([]protocol.ThreadID, error) {
	edges := make(map[protocol.ThreadID]protocol.AgentSpawnEdgeState)
	for _, line := range lines {
		edge, ok := line.Item.(rollout.AgentSpawnEdgeItem)
		if !ok {
			continue
		}
		if edge.ParentThreadID != rootID {
			return nil, fmt.Errorf("agent edge %q belongs to parent %q, expected %q", edge.AgentID, edge.ParentThreadID, rootID)
		}
		edges[edge.AgentID] = edge.State
	}
	result := make([]protocol.ThreadID, 0, len(edges))
	for id, state := range edges {
		if state == protocol.AgentSpawnEdgeOpen {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].String() < result[right].String() })
	return result, nil
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
	live, history, err := threadstore.NewResumedLiveThread(ctx, id, manager.store)
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
	history, err = threadstore.RecoverInterruptedTurn(recoveryCtx, live, history)
	cancel()
	if err != nil {
		return nil, err
	}
	if _, err := canonicalChildMeta(history, control.SessionID(), id, record.Metadata.ParentThreadID); err != nil {
		return nil, err
	}
	configuration := parent.session.Configuration()
	configuration.Source = stored.Source.Clone()
	configuration.Mode = protocol.ModeKindDefault
	parentID := record.Metadata.ParentThreadID
	child, err := manager.spawn(ctx, control.SessionID(), id, &parentID, live, history, StartInput{Configuration: configuration}, control, false)
	if err != nil {
		return nil, err
	}
	cleanup = false
	return child, nil
}

func canonicalChildMeta(history threadstore.InitialHistory, sessionID protocol.SessionID, id, parentID protocol.ThreadID) (rollout.SessionMetaItem, error) {
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

func persistedAgentLifecycle(history threadstore.InitialHistory) multiagent.LifecycleState {
	state := multiagent.InitialLifecycleState()
	for _, line := range history.Lines {
		event, ok := line.Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		if next, changed, _ := multiagent.ReduceLifecycleEvent(state, event.Msg); changed {
			state = next
		}
	}
	if state.Status.Kind == protocol.AgentStatusRunning {
		turnID := protocol.TurnID("")
		for index := len(history.Lines) - 1; index >= 0; index-- {
			event, ok := history.Lines[index].Item.(rollout.EventMsgItem)
			if !ok {
				continue
			}
			if started, ok := event.Msg.(protocol.TurnStartedEvent); ok {
				turnID = started.TurnID
				break
			}
		}
		result := protocol.AgentTurnResult{
			TurnID: turnID, Outcome: protocol.TurnOutcomeAborted,
			Reason: "previous process ended before the turn completed",
		}
		state = multiagent.LifecycleState{Status: protocol.AgentStatus{Kind: protocol.AgentStatusInterrupted}, LastTurn: &result}
	}
	return state
}
