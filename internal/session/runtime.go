package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
)

type SessionRuntime struct {
	coordinator      *Coordinator
	extensions       io.Closer
	extensionFactory func() (io.Closer, error)
	extensionSession SessionID
	permissions      *project.PermissionStore
	approvals        *policy.SessionApprovalStore

	mutex        sync.RWMutex
	history      *SessionHistory
	activeRun    RunID
	activeCancel context.CancelCauseFunc
	closed       bool
}

func NewSessionRuntime(coordinator *Coordinator) (*SessionRuntime, error) {
	return NewSessionRuntimeWithOptions(coordinator, SessionRuntimeOptions{})
}

type SessionRuntimeOptions struct {
	Extensions       io.Closer
	ExtensionFactory func() (io.Closer, error)
}

func NewSessionRuntimeWithOptions(coordinator *Coordinator, options SessionRuntimeOptions) (*SessionRuntime, error) {
	if coordinator == nil {
		return nil, errors.New("session runtime coordinator is nil")
	}
	return &SessionRuntime{
		coordinator: coordinator, extensions: options.Extensions, extensionFactory: options.ExtensionFactory,
		permissions: project.NewPermissionStore(), approvals: policy.NewSessionApprovalStore(),
	}, nil
}

func (runtime *SessionRuntime) ListSessions(ctx context.Context) ([]Session, error) {
	if err := runtime.validateOpen(); err != nil {
		return nil, err
	}
	return runtime.coordinator.ListSessions(ctx)
}

func (runtime *SessionRuntime) Continue(ctx context.Context) (Session, error) {
	if err := runtime.validateOpen(); err != nil {
		return Session{}, err
	}
	value, err := runtime.coordinator.Continue(ctx)
	if err != nil {
		return Session{}, err
	}
	if err := runtime.switchExtensions(value.ID); err != nil {
		return Session{}, err
	}
	return value, runtime.loadHistory(ctx, value)
}

func (runtime *SessionRuntime) Resume(ctx context.Context, id SessionID) (Session, error) {
	if err := runtime.validateOpen(); err != nil {
		return Session{}, err
	}
	value, err := runtime.coordinator.Resume(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if err := runtime.switchExtensions(value.ID); err != nil {
		return Session{}, err
	}
	return value, runtime.loadHistory(ctx, value)
}

func (runtime *SessionRuntime) BeginRun(ctx context.Context, objective string, metadata RunMetadata) (StartedRun, error) {
	if err := runtime.validateOpen(); err != nil {
		return StartedRun{}, err
	}
	runtime.mutex.Lock()
	if runtime.activeRun != "" {
		runtime.mutex.Unlock()
		return StartedRun{}, errors.New("session runtime already has an active Run")
	}
	runtime.mutex.Unlock()
	if err := runtime.ensureExtensions(); err != nil {
		return StartedRun{}, err
	}

	started, err := runtime.coordinator.BeginRun(ctx, objective, metadata)
	if err != nil {
		return StartedRun{}, err
	}
	items := append(cloneItems(started.PriorItems), started.Records.Item)
	history, err := NewSessionHistory(started.Records.Session, items)
	if err != nil {
		return StartedRun{}, err
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	if runtime.closed {
		return StartedRun{}, errors.New("session runtime is closed")
	}
	if runtime.activeRun != "" {
		return StartedRun{}, errors.New("session runtime acquired another active Run")
	}
	runtime.history = history
	runtime.activeRun = started.Records.Run.ID
	if runtime.extensionSession == "" {
		runtime.extensionSession = started.Records.Session.ID
	}
	return started, nil
}

func (runtime *SessionRuntime) AttachCancel(runID RunID, cancel context.CancelCauseFunc) error {
	if cancel == nil {
		return errors.New("session runtime cancel function is nil")
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	if runtime.closed {
		return errors.New("session runtime is closed")
	}
	if runtime.activeRun != runID || runID == "" {
		return errors.New("session runtime cancel does not match active Run")
	}
	runtime.activeCancel = cancel
	return nil
}

func (runtime *SessionRuntime) CancelActive(cause error) bool {
	runtime.mutex.RLock()
	cancel := runtime.activeCancel
	runtime.mutex.RUnlock()
	if cancel == nil {
		return false
	}
	cancel(cause)
	return true
}

func (runtime *SessionRuntime) Append(ctx context.Context, runID RunID, drafts ...AppendItem) ([]RolloutItem, error) {
	runtime.mutex.RLock()
	if runtime.closed || runtime.activeRun != runID || runtime.history == nil {
		runtime.mutex.RUnlock()
		return nil, errors.New("session runtime append requires the active Run")
	}
	sessionID := runtime.history.View().Session.ID
	runtime.mutex.RUnlock()
	items, err := runtime.coordinator.AppendItems(ctx, sessionID, drafts...)
	if err != nil {
		return nil, err
	}
	value, err := runtime.coordinator.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	runtime.mutex.RLock()
	history := runtime.history
	runtime.mutex.RUnlock()
	if history == nil {
		return nil, errors.New("session runtime history disappeared")
	}
	if err := history.Append(value, items...); err != nil {
		return nil, err
	}
	return cloneItems(items), nil
}

func (runtime *SessionRuntime) FinishRun(ctx context.Context, started StartedRun, status RunStatus, stopReason, assistantContent string, usage json.RawMessage) (FinishRunResult, error) {
	runtime.mutex.RLock()
	if runtime.closed || runtime.activeRun != started.Records.Run.ID || runtime.history == nil {
		runtime.mutex.RUnlock()
		return FinishRunResult{}, errors.New("session runtime finish requires the active Run")
	}
	history := runtime.history
	runtime.mutex.RUnlock()
	result, err := runtime.coordinator.FinishRun(ctx, started, status, stopReason, assistantContent, usage)
	if err != nil {
		return FinishRunResult{}, err
	}
	if err := history.Append(result.Session, result.Items...); err != nil {
		return FinishRunResult{}, err
	}
	runtime.mutex.Lock()
	if runtime.activeRun == started.Records.Run.ID {
		runtime.activeRun = ""
		runtime.activeCancel = nil
	}
	runtime.mutex.Unlock()
	return result, nil
}

func (runtime *SessionRuntime) History() HistoryView {
	if runtime == nil {
		return HistoryView{}
	}
	runtime.mutex.RLock()
	history := runtime.history
	runtime.mutex.RUnlock()
	if history == nil {
		return HistoryView{}
	}
	return history.View()
}

func (runtime *SessionRuntime) ActiveRunID() RunID {
	if runtime == nil {
		return ""
	}
	runtime.mutex.RLock()
	defer runtime.mutex.RUnlock()
	return runtime.activeRun
}

func (runtime *SessionRuntime) CurrentSessionID() SessionID {
	if runtime == nil || runtime.coordinator == nil {
		return ""
	}
	return runtime.coordinator.CurrentSessionID()
}

func (runtime *SessionRuntime) PermissionStore() *project.PermissionStore {
	if runtime == nil {
		return nil
	}
	return runtime.permissions
}

func (runtime *SessionRuntime) ApprovalStore() *policy.SessionApprovalStore {
	if runtime == nil {
		return nil
	}
	return runtime.approvals
}

func (runtime *SessionRuntime) Extension() io.Closer {
	if runtime == nil {
		return nil
	}
	runtime.mutex.RLock()
	defer runtime.mutex.RUnlock()
	return runtime.extensions
}

func (runtime *SessionRuntime) Close() error {
	if runtime == nil {
		return nil
	}
	runtime.mutex.Lock()
	if runtime.closed {
		runtime.mutex.Unlock()
		return nil
	}
	runtime.closed = true
	cancel := runtime.activeCancel
	extensions := runtime.extensions
	runtime.activeCancel = nil
	runtime.extensions = nil
	runtime.extensionSession = ""
	runtime.mutex.Unlock()
	runtime.permissions.Clear()
	runtime.approvals.Clear()
	if cancel != nil {
		cancel(errors.New("session runtime closed"))
	}
	if extensions != nil {
		return extensions.Close()
	}
	return nil
}

func (runtime *SessionRuntime) ensureExtensions() error {
	runtime.mutex.RLock()
	configured := runtime.extensions != nil || runtime.extensionFactory == nil
	runtime.mutex.RUnlock()
	if configured {
		return nil
	}
	return runtime.switchExtensions("")
}

func (runtime *SessionRuntime) switchExtensions(sessionID SessionID) error {
	runtime.mutex.RLock()
	if runtime.closed {
		runtime.mutex.RUnlock()
		return errors.New("session runtime is closed")
	}
	if runtime.extensions != nil && (runtime.extensionSession == sessionID || runtime.extensionFactory == nil) {
		runtime.mutex.RUnlock()
		return nil
	}
	factory := runtime.extensionFactory
	runtime.mutex.RUnlock()
	if factory == nil {
		return nil
	}
	replacement, err := factory()
	if err != nil {
		return fmt.Errorf("create Session extension runtime: %w", err)
	}
	if replacement == nil {
		return errors.New("Session extension factory returned nil")
	}
	runtime.mutex.Lock()
	if runtime.closed {
		runtime.mutex.Unlock()
		_ = replacement.Close()
		return errors.New("session runtime is closed")
	}
	previous := runtime.extensions
	previousSession := runtime.extensionSession
	runtime.extensions = replacement
	runtime.extensionSession = sessionID
	runtime.mutex.Unlock()
	if previousSession != "" && previousSession != sessionID {
		runtime.permissions.Clear()
		runtime.approvals.Clear()
	}
	if previous != nil {
		return previous.Close()
	}
	return nil
}

func (runtime *SessionRuntime) loadHistory(ctx context.Context, value Session) error {
	items, err := runtime.coordinator.store.ListItems(ctx, value.ID)
	if err != nil {
		return err
	}
	history, err := NewSessionHistory(value, items)
	if err != nil {
		return err
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	if runtime.closed {
		return errors.New("session runtime is closed")
	}
	if runtime.activeRun != "" {
		return errors.New("cannot switch Session while a Run is active")
	}
	runtime.history = history
	return nil
}

func (runtime *SessionRuntime) validateOpen() error {
	if runtime == nil || runtime.coordinator == nil {
		return errors.New("session runtime is nil")
	}
	runtime.mutex.RLock()
	defer runtime.mutex.RUnlock()
	if runtime.closed {
		return errors.New("session runtime is closed")
	}
	return nil
}
