package app

import (
	"context"
	"errors"
	"sync"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

var ErrNoActiveThread = errors.New("no active thread")

// ThreadWorkspace owns the application-level selection of the current Thread.
// ThreadManager owns all live Threads; interfaces only choose which one is
// currently attached to the user session through this service.
type ThreadWorkspace struct {
	mu      sync.RWMutex
	manager *threadmanager.ThreadManager
	current *threadmanager.AmadeusThread
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

func (workspace *ThreadWorkspace) Resume(ctx context.Context, id thread.ID, configuration agentsession.Configuration) (*threadmanager.AmadeusThread, error) {
	manager := workspace.managerSnapshot()
	if manager == nil {
		return nil, errors.New("thread workspace is closed")
	}
	if current, ok := workspace.Current(); ok && current.ID() == id {
		return current, nil
	}
	if current, ok := workspace.Current(); ok {
		if err := manager.ShutdownThread(ctx, current.ID()); err != nil {
			return nil, err
		}
		workspace.mu.Lock()
		if workspace.current == current {
			workspace.current = nil
		}
		workspace.mu.Unlock()
	}
	resumed, err := manager.ResumeThread(ctx, id, threadmanager.StartInput{Configuration: configuration})
	if err != nil {
		return nil, err
	}
	workspace.mu.Lock()
	workspace.current = resumed
	workspace.mu.Unlock()
	return resumed, nil
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

func (workspace *ThreadWorkspace) DeleteCurrent(ctx context.Context) (thread.ID, error) {
	manager := workspace.managerSnapshot()
	if manager == nil {
		return "", errors.New("thread workspace is closed")
	}
	workspace.mu.Lock()
	current := workspace.current
	workspace.current = nil
	workspace.mu.Unlock()
	if current == nil {
		return "", ErrNoActiveThread
	}
	if err := manager.DeleteThread(ctx, current.ID()); err != nil {
		workspace.mu.Lock()
		workspace.current = current
		workspace.mu.Unlock()
		return "", err
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
