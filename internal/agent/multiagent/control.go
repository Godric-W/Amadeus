package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func NewControl(sessionID protocol.SessionID, rootID protocol.ThreadID, host AgentHost, options Options) (*Control, error) {
	if sessionID.IsZero() || rootID.IsZero() || host == nil {
		return nil, errors.New("agent control is incomplete")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Control{
		host: host, sessionID: sessionID, rootID: rootID, maxAgents: options.MaxAgents, maxDepth: options.MaxDepth,
		agents: make(map[protocol.ThreadID]*record), reservations: make(map[string]reservation),
		nicknames: make(map[string]struct{}), changed: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

func (control *Control) SessionID() protocol.SessionID {
	if control == nil {
		return protocol.SessionID{}
	}
	return control.sessionID
}

func (control *Control) RootThreadID() protocol.ThreadID {
	if control == nil {
		return protocol.ThreadID{}
	}
	return control.rootID
}

func (control *Control) Spawn(ctx context.Context, parentID protocol.ThreadID, message string) (result SpawnResult, err error) {
	if control == nil {
		return SpawnResult{}, errors.New("agent control is nil")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return SpawnResult{}, errors.New("spawn agent message is empty")
	}
	depth, err := control.childDepth(parentID)
	if err != nil {
		return SpawnResult{}, err
	}
	reservationID, nickname, err := control.reserve()
	if err != nil {
		return SpawnResult{}, err
	}
	committed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			control.rollbackReservation(reservationID)
			panic(recovered)
		}
		if !committed {
			control.rollbackReservation(reservationID)
		}
	}()
	runtime, err := control.host.SpawnChild(ctx, control, SpawnChildRequest{
		ParentThreadID: parentID, Depth: depth, Nickname: nickname, Role: ExplorerRole, Message: message,
	})
	if err != nil {
		return SpawnResult{}, err
	}
	if runtime == nil || runtime.ID().IsZero() {
		if runtime != nil {
			_ = shutdownRuntime(ctx, runtime)
		}
		return SpawnResult{}, errors.New("spawn child returned an invalid runtime")
	}
	metadata := protocol.AgentMetadata{
		ThreadID: runtime.ID(), ParentThreadID: parentID, Depth: depth, AgentNickname: nickname, AgentRole: ExplorerRole,
	}
	if err := metadata.Validate(); err != nil {
		_ = shutdownRuntime(ctx, runtime)
		return SpawnResult{}, err
	}
	if err := control.installReservation(reservationID, metadata, runtime); err != nil {
		_ = shutdownRuntime(ctx, runtime)
		return SpawnResult{}, err
	}
	go control.consume(runtime.ID(), runtime)
	edgeOpen := false
	defer func() {
		if !committed {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if edgeOpen {
				_ = control.host.RecordSpawnEdge(cleanupCtx, parentID, runtime.ID(), protocol.AgentSpawnEdgeClosed)
			}
			_ = control.closeAgent(cleanupCtx, runtime.ID(), closeModeRollback)
		}
	}()
	if err := runtime.SubmitUserInput(ctx, protocol.UserInputOp{Content: message}); err != nil {
		return SpawnResult{}, fmt.Errorf("submit initial sub-agent input: %w", err)
	}
	if err := control.host.RecordSpawnEdge(ctx, parentID, runtime.ID(), protocol.AgentSpawnEdgeOpen); err != nil {
		return SpawnResult{}, fmt.Errorf("persist open agent edge: %w", err)
	}
	edgeOpen = true
	notification, err := control.commitInstalledAgent(runtime.ID())
	if err != nil {
		return SpawnResult{}, err
	}
	committed = true
	control.deliverNotification(notification)
	return SpawnResult{AgentID: runtime.ID(), Nickname: nickname}, nil
}

func (control *Control) SendInput(ctx context.Context, id protocol.ThreadID, message string, interrupt bool) error {
	if control == nil {
		return errors.New("agent control is nil")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return errors.New("send input message is empty")
	}
	runtime, status, err := control.runtimeForInput(ctx, id)
	if err != nil {
		return err
	}
	if interrupt && status.Kind == protocol.AgentStatusRunning {
		if err := runtime.Submit(ctx, protocol.InterruptOp{}); err != nil {
			return fmt.Errorf("interrupt agent %q: %w", id, err)
		}
		if err := control.waitUntilNotRunning(ctx, id); err != nil {
			return fmt.Errorf("agent %q did not stop its active turn before restart", id)
		}
	}
	if err := runtime.SubmitUserInput(ctx, protocol.UserInputOp{Content: message}); err != nil {
		return fmt.Errorf("send input to agent %q: %w", id, err)
	}
	return nil
}

func (control *Control) Wait(ctx context.Context, ids []protocol.ThreadID, timeout time.Duration) (WaitResult, error) {
	if control == nil {
		return WaitResult{}, errors.New("agent control is nil")
	}
	if len(ids) == 0 {
		return WaitResult{}, errors.New("wait agent IDs are empty")
	}
	ids = uniqueIDs(ids)
	if timeout < 0 {
		return WaitResult{}, errors.New("wait timeout is negative")
	}
	if timeout == 0 {
		timeout = defaultWaitTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		control.retryPendingNotifications(ids)
		snapshots, ready, changed := control.waitSnapshot(ids)
		if ready {
			return WaitResult{Statuses: snapshots}, nil
		}
		select {
		case <-changed:
		case <-timer.C:
			return WaitResult{TimedOut: true}, nil
		case <-ctx.Done():
			return WaitResult{}, ctx.Err()
		}
	}
}

func (control *Control) retryPendingNotifications(ids []protocol.ThreadID) {
	control.mu.Lock()
	notifications := make([]*Notification, 0, len(ids))
	for _, id := range ids {
		if notification := pendingNotificationLocked(control.agents[id]); notification != nil {
			notifications = append(notifications, notification)
		}
	}
	control.mu.Unlock()
	for _, notification := range notifications {
		control.deliverNotification(notification)
	}
}

func (control *Control) waitUntilNotRunning(ctx context.Context, id protocol.ThreadID) error {
	for {
		control.mu.Lock()
		snapshot := control.snapshotLocked(id)
		changed := control.changed
		control.mu.Unlock()
		if !snapshot.Status.IsRunning() {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (control *Control) CloseAgent(ctx context.Context, id protocol.ThreadID) (protocol.AgentStatus, error) {
	if control == nil {
		return protocol.AgentStatus{}, errors.New("agent control is nil")
	}
	previous := control.Snapshot(id).Status
	if previous.Kind == protocol.AgentStatusNotFound {
		return previous, nil
	}
	return previous, control.closeAgent(ctx, id, closeModeExplicit)
}

func (control *Control) Close(ctx context.Context) error {
	if control == nil {
		return nil
	}
	control.closeOnce.Do(func() {
		defer close(control.done)
		control.mu.Lock()
		control.closed = true
		ids := make([]protocol.ThreadID, 0, len(control.agents))
		for id := range control.agents {
			ids = append(ids, id)
		}
		control.signalLocked()
		control.mu.Unlock()
		for _, id := range ids {
			control.closeErr = errors.Join(control.closeErr, control.closeAgent(ctx, id, closeModeUnload))
		}
	})
	return control.closeErr
}

func (control *Control) Snapshot(id protocol.ThreadID) StatusSnapshot {
	if control == nil {
		return notFoundSnapshot(id)
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	return control.snapshotLocked(id)
}

func (control *Control) Record(id protocol.ThreadID) (AgentRecord, bool) {
	if control == nil {
		return AgentRecord{}, false
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	agent := control.agents[id]
	if agent == nil {
		return AgentRecord{}, false
	}
	return AgentRecord{Metadata: agent.metadata, Status: agent.status, LastTurn: cloneTurnResult(agent.lastTurn)}, true
}

func (control *Control) SnapshotAll() []AgentRecord {
	if control == nil {
		return nil
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	values := make([]AgentRecord, 0, len(control.agents))
	for _, agent := range control.agents {
		values = append(values, AgentRecord{Metadata: agent.metadata, Status: agent.status, LastTurn: cloneTurnResult(agent.lastTurn)})
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left].Metadata.ThreadID.String() < values[right].Metadata.ThreadID.String()
	})
	return values
}
