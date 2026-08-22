package app

import (
	"context"
	"errors"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/state"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

var ErrNoActiveThread = errors.New("no active thread")
var ErrThreadSelectionChanged = errors.New("active thread changed while preparing switch")

// ThreadWorkspace owns the application-level selection of the current Thread.
// ThreadManager owns all live Threads; interfaces only choose which one is
// currently attached to the user session through this service.
type ThreadWorkspace struct {
	mu      sync.RWMutex
	manager *threadmanager.ThreadManager
	current *threadmanager.AmadeusThread
}

type PreparedThreadSwitch struct {
	workspace *ThreadWorkspace
	target    *threadmanager.AmadeusThread
	previous  *threadmanager.AmadeusThread
	committed bool
}

func (prepared *PreparedThreadSwitch) Target() *threadmanager.AmadeusThread {
	if prepared == nil {
		return nil
	}
	return prepared.target
}

func (prepared *PreparedThreadSwitch) Previous() *threadmanager.AmadeusThread {
	if prepared == nil {
		return nil
	}
	return prepared.previous
}

func (prepared *PreparedThreadSwitch) Commit() error {
	if prepared == nil || prepared.workspace == nil || prepared.target == nil {
		return errors.New("prepared thread switch is incomplete")
	}
	prepared.workspace.mu.Lock()
	defer prepared.workspace.mu.Unlock()
	if prepared.workspace.current != prepared.previous {
		return ErrThreadSelectionChanged
	}
	prepared.workspace.current = prepared.target
	prepared.committed = true
	return nil
}

func (prepared *PreparedThreadSwitch) Abort(ctx context.Context) error {
	if prepared == nil || prepared.workspace == nil || prepared.target == nil || prepared.committed || prepared.target == prepared.previous {
		return nil
	}
	manager := prepared.workspace.managerSnapshot()
	if manager == nil {
		return errors.New("thread workspace is closed")
	}
	return manager.ShutdownThread(ctx, prepared.target.ID())
}

func NewThreadWorkspace(manager *threadmanager.ThreadManager) (*ThreadWorkspace, error) {
	if manager == nil {
		return nil, errors.New("thread workspace manager is nil")
	}
	return &ThreadWorkspace{manager: manager}, nil
}

func (workspace *ThreadWorkspace) managerSnapshot() *threadmanager.ThreadManager {
	if workspace == nil {
		return nil
	}
	workspace.mu.RLock()
	defer workspace.mu.RUnlock()
	return workspace.manager
}

func (workspace *ThreadWorkspace) Current() (*threadmanager.AmadeusThread, bool) {
	if workspace == nil {
		return nil, false
	}
	workspace.mu.RLock()
	defer workspace.mu.RUnlock()
	return workspace.current, workspace.current != nil
}

func (workspace *ThreadWorkspace) EnsureCurrent(ctx context.Context, configuration agentsession.Configuration) (*threadmanager.AmadeusThread, error) {
	if current, ok := workspace.Current(); ok {
		return current, nil
	}
	manager := workspace.managerSnapshot()
	if manager == nil {
		return nil, errors.New("thread workspace is closed")
	}
	created, err := manager.StartThread(ctx, threadmanager.StartInput{Configuration: configuration})
	if err != nil {
		return nil, err
	}
	workspace.mu.Lock()
	if workspace.current == nil {
		workspace.current = created
		workspace.mu.Unlock()
		return created, nil
	}
	current := workspace.current
	workspace.mu.Unlock()
	_ = created.Shutdown(context.Background())
	return current, nil
}

func (workspace *ThreadWorkspace) Resume(ctx context.Context, id protocol.ThreadID, configuration agentsession.Configuration) (*threadmanager.AmadeusThread, error) {
	prepared, err := workspace.PrepareResume(ctx, id, configuration)
	if err != nil {
		return nil, err
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		return nil, err
	}
	if previous := prepared.Previous(); previous != nil && previous != prepared.Target() {
		manager := workspace.managerSnapshot()
		if manager != nil {
			if err := manager.ShutdownThread(ctx, previous.ID()); err != nil {
				return prepared.Target(), err
			}
		}
	}
	return prepared.Target(), nil
}

func (workspace *ThreadWorkspace) PrepareResume(ctx context.Context, id protocol.ThreadID, configuration agentsession.Configuration) (*PreparedThreadSwitch, error) {
	manager := workspace.managerSnapshot()
	if manager == nil {
		return nil, errors.New("thread workspace is closed")
	}
	current, _ := workspace.Current()
	if current != nil && current.ID() == id {
		return &PreparedThreadSwitch{workspace: workspace, target: current, previous: current}, nil
	}
	resumed, err := manager.ResumeThread(ctx, id, threadmanager.StartInput{Configuration: configuration})
	if err != nil {
		return nil, err
	}
	return &PreparedThreadSwitch{workspace: workspace, target: resumed, previous: current}, nil
}

func (workspace *ThreadWorkspace) PrepareNew(ctx context.Context, configuration agentsession.Configuration) (*PreparedThreadSwitch, error) {
	manager := workspace.managerSnapshot()
	if manager == nil {
		return nil, errors.New("thread workspace is closed")
	}
	current, _ := workspace.Current()
	created, err := manager.StartThread(ctx, threadmanager.StartInput{Configuration: configuration})
	if err != nil {
		return nil, err
	}
	return &PreparedThreadSwitch{workspace: workspace, target: created, previous: current}, nil
}

func (workspace *ThreadWorkspace) Release(ctx context.Context, value *threadmanager.AmadeusThread) error {
	if value == nil {
		return nil
	}
	manager := workspace.managerSnapshot()
	if manager == nil {
		return errors.New("thread workspace is closed")
	}
	return manager.ShutdownThread(ctx, value.ID())
}

func (workspace *ThreadWorkspace) NewDraft(ctx context.Context) error {
	if workspace == nil {
		return errors.New("thread workspace is nil")
	}
	workspace.mu.Lock()
	current := workspace.current
	workspace.current = nil
	manager := workspace.manager
	workspace.mu.Unlock()
	if current == nil || manager == nil {
		return nil
	}
	return manager.ShutdownThread(ctx, current.ID())
}

func (workspace *ThreadWorkspace) RenameCurrent(ctx context.Context, title string) error {
	manager := workspace.managerSnapshot()
	current, ok := workspace.Current()
	if manager == nil || !ok {
		return ErrNoActiveThread
	}
	return manager.RenameThread(ctx, current.ID(), title)
}

func (workspace *ThreadWorkspace) DeleteCurrent(ctx context.Context) (protocol.ThreadID, error) {
	manager := workspace.managerSnapshot()
	if manager == nil {
		return protocol.ThreadID{}, errors.New("thread workspace is closed")
	}
	workspace.mu.Lock()
	current := workspace.current
	workspace.current = nil
	workspace.mu.Unlock()
	if current == nil {
		return protocol.ThreadID{}, ErrNoActiveThread
	}
	if err := manager.DeleteThread(ctx, current.ID()); err != nil {
		workspace.mu.Lock()
		workspace.current = current
		workspace.mu.Unlock()
		return protocol.ThreadID{}, err
	}
	return current.ID(), nil
}

func (workspace *ThreadWorkspace) List(ctx context.Context, query state.ListQuery) ([]state.StoredThread, error) {
	manager := workspace.managerSnapshot()
	if manager == nil {
		return nil, errors.New("thread workspace is closed")
	}
	return manager.ListThreads(ctx, query)
}

func (workspace *ThreadWorkspace) CurrentMetadata(ctx context.Context) (state.StoredThread, error) {
	current, ok := workspace.Current()
	if !ok {
		return state.StoredThread{}, state.ErrNotFound
	}
	threads, err := workspace.List(ctx, state.ListQuery{IncludeArchived: true})
	if err != nil {
		return state.StoredThread{}, err
	}
	for _, metadata := range threads {
		if metadata.ID == current.ID() {
			return metadata, nil
		}
	}
	return state.StoredThread{}, state.ErrNotFound
}

func (workspace *ThreadWorkspace) Close(ctx context.Context) error {
	if workspace == nil {
		return nil
	}
	workspace.mu.Lock()
	manager := workspace.manager
	workspace.manager = nil
	workspace.current = nil
	workspace.mu.Unlock()
	if manager == nil {
		return nil
	}
	return manager.Close(ctx)
}
