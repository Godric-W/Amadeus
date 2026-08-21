package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func NewControl(rootID protocol.ThreadID, host AgentHost, options Options) (*Control, error) {
	if strings.TrimSpace(string(rootID)) == "" || host == nil {
		return nil, errors.New("agent control is incomplete")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Control{
		host: host, rootID: rootID, maxAgents: options.MaxAgents, maxDepth: options.MaxDepth,
		agents: make(map[protocol.ThreadID]*record), reservations: make(map[string]reservation),
		nicknames: make(map[string]struct{}), changed: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

func (control *Control) RootID() protocol.ThreadID {
	if control == nil {
		return ""
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
	if runtime == nil || strings.TrimSpace(string(runtime.ID())) == "" {
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
	if err := control.commitReservation(reservationID, metadata, runtime); err != nil {
		_ = shutdownRuntime(ctx, runtime)
		return SpawnResult{}, err
	}
	committed = true
	go control.consume(runtime.ID(), runtime)
	if err := runtime.SubmitUserInput(ctx, protocol.UserInputOp{Content: message}); err != nil {
		_ = control.closeAgent(ctx, runtime.ID())
		return SpawnResult{}, fmt.Errorf("submit initial sub-agent input: %w", err)
	}
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
	runtime, status, err := control.runtimeForInput(id)
	if err != nil {
		return err
	}
	if interrupt && status.Kind == protocol.AgentStatusRunning {
		if err := runtime.Submit(ctx, protocol.InterruptOp{}); err != nil {
			return fmt.Errorf("interrupt agent %q: %w", id, err)
		}
		waited, err := control.Wait(ctx, []protocol.ThreadID{id}, 0)
		if err != nil {
			return err
		}
		if len(waited.Statuses) != 1 || waited.Statuses[0].Status.IsRunning() {
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
		snapshots, allReady, changed := control.waitSnapshot(ids)
		if allReady {
			return WaitResult{Statuses: snapshots}, nil
		}
		select {
		case <-changed:
		case <-timer.C:
			return WaitResult{Statuses: control.snapshots(ids), TimedOut: true}, nil
		case <-ctx.Done():
			return WaitResult{}, ctx.Err()
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
	return previous, control.closeAgent(ctx, id)
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
			control.closeErr = errors.Join(control.closeErr, control.closeAgent(ctx, id))
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

func (control *Control) SnapshotAll() []AgentRecord {
	if control == nil {
		return nil
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	values := make([]AgentRecord, 0, len(control.agents))
	for _, agent := range control.agents {
		values = append(values, AgentRecord{Metadata: agent.metadata, Status: agent.status})
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left].Metadata.ThreadID < values[right].Metadata.ThreadID
	})
	return values
}
