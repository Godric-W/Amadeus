package manager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
)

type SharedServices struct {
	DefaultTaskFactory task.Factory
	NewTaskFactory     func(thread.ID) (task.Factory, error)
	Clock              func() time.Time
	NextID             func(string) string
}

type StartInput struct {
	Configuration agentsession.Configuration
}

type ThreadManager struct {
	lifecycle sync.RWMutex
	mu        sync.Mutex
	ctx       context.Context
	store     thread.ThreadStore
	services  SharedServices
	threads   map[thread.ID]*AmadeusThread
	closed    bool
}

type AmadeusThread struct {
	id      thread.ID
	session *agentsession.Session
	io      agentsession.SessionIo
}

func New(ctx context.Context, store thread.ThreadStore, services SharedServices) (*ThreadManager, error) {
	if ctx == nil || store == nil || services.NextID == nil {
		return nil, errors.New("thread manager is incomplete")
	}
	if services.Clock == nil {
		services.Clock = time.Now
	}
	return &ThreadManager{ctx: ctx, store: store, services: services, threads: make(map[thread.ID]*AmadeusThread)}, nil
}

func (manager *ThreadManager) StartThread(ctx context.Context, input StartInput) (*AmadeusThread, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	id := thread.ID(manager.services.NextID("thread"))
	live, err := thread.NewDraftLiveThread(id, manager.store)
	if err != nil {
		return nil, err
	}
	return manager.spawn(ctx, id, live, thread.InitialHistory{Kind: thread.InitialHistoryNew}, input)
}

func (manager *ThreadManager) ResumeThread(ctx context.Context, id thread.ID, input StartInput) (*AmadeusThread, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	manager.mu.Lock()
	if existing := manager.threads[id]; existing != nil {
		manager.mu.Unlock()
		return existing, nil
	}
	manager.mu.Unlock()
	live, history, err := thread.NewResumedLiveThread(ctx, id, manager.store)
	if err != nil {
		return nil, err
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	history, err = thread.RecoverInterruptedTurn(recoveryCtx, live, history)
	cancel()
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	return manager.spawn(ctx, id, live, history, input)
}

func (manager *ThreadManager) spawn(ctx context.Context, id thread.ID, live *thread.LiveThread, history thread.InitialHistory, input StartInput) (*AmadeusThread, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tasks := manager.services.DefaultTaskFactory
	if manager.services.NewTaskFactory != nil {
		var err error
		tasks, err = manager.services.NewTaskFactory(id)
		if err != nil {
			_ = live.Shutdown(context.Background())
			return nil, err
		}
	}
	if tasks == nil {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("thread task factory is unavailable")
	}
	session, io, err := agentsession.Spawn(manager.ctx, agentsession.SpawnArgs{
		ThreadID: id, History: history,
		State:    agentsession.SessionState{Configuration: input.Configuration},
		Services: agentsession.SessionServices{LiveThread: live, TaskFactory: tasks, Clock: manager.services.Clock, NextID: manager.services.NextID},
	})
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	value := &AmadeusThread{id: id, session: session, io: io}
	manager.mu.Lock()
	if existing := manager.threads[id]; existing != nil {
		manager.mu.Unlock()
		_ = value.Shutdown(context.Background())
		return existing, nil
	}
	manager.threads[id] = value
	manager.mu.Unlock()
	go func() {
		<-io.Terminated
		manager.mu.Lock()
		if manager.threads[id] == value {
			delete(manager.threads, id)
		}
		manager.mu.Unlock()
	}()
	return value, nil
}

func (manager *ThreadManager) GetThread(id thread.ID) (*AmadeusThread, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	value, ok := manager.threads[id]
	return value, ok
}

func (manager *ThreadManager) ListThreads(ctx context.Context, query state.ListQuery) ([]state.StoredThread, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	return manager.store.ListThreads(ctx, query)
}

func (manager *ThreadManager) RenameThread(ctx context.Context, id thread.ID, title string) error {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return errors.New("thread manager is closed")
	}
	manager.mu.Lock()
	value := manager.threads[id]
	manager.mu.Unlock()
	if value != nil {
		return value.sessionRename(ctx, title, manager.services.Clock().UTC())
	}
	return manager.store.RenameThread(ctx, id, title, manager.services.Clock().UTC())
}

func (manager *ThreadManager) DeleteThread(ctx context.Context, id thread.ID) error {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return errors.New("thread manager is closed")
	}
	if value, ok := manager.GetThread(id); ok {
		if err := value.Shutdown(ctx); err != nil {
			return err
		}
	}
	return manager.store.DeleteThread(ctx, id, manager.services.Clock().UTC())
}

func (manager *ThreadManager) ShutdownThread(ctx context.Context, id thread.ID) error {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil
	}
	value, ok := manager.GetThread(id)
	if !ok {
		return nil
	}
	return value.Shutdown(ctx)
}

func (manager *ThreadManager) Close(ctx context.Context) error {
	manager.lifecycle.Lock()
	defer manager.lifecycle.Unlock()
	if manager.closed {
		return nil
	}
	manager.closed = true
	manager.mu.Lock()
	threads := make([]*AmadeusThread, 0, len(manager.threads))
	for _, value := range manager.threads {
		threads = append(threads, value)
	}
	manager.mu.Unlock()
	var result error
	for _, value := range threads {
		result = errors.Join(result, value.Shutdown(ctx))
	}
	return errors.Join(result, manager.store.Close())
}

func (threadRuntime *AmadeusThread) ID() thread.ID {
	if threadRuntime == nil {
		return ""
	}
	return threadRuntime.id
}

func (threadRuntime *AmadeusThread) Io() agentsession.SessionIo {
	if threadRuntime == nil {
		return agentsession.SessionIo{}
	}
	return threadRuntime.io
}

func (threadRuntime *AmadeusThread) History() []rollout.Line {
	if threadRuntime == nil || threadRuntime.session == nil {
		return nil
	}
	return threadRuntime.session.History()
}

func (threadRuntime *AmadeusThread) Submit(ctx context.Context, op protocol.Op) error {
	if threadRuntime == nil || op == nil {
		return errors.New("thread submission is empty")
	}
	select {
	case threadRuntime.io.Submissions <- protocol.Submission{Op: op}:
		return nil
	case <-threadRuntime.io.Terminated:
		return errors.New("thread is terminated")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (threadRuntime *AmadeusThread) Request(ctx context.Context, request protocol.InteractiveRequest) (protocol.Op, error) {
	if threadRuntime == nil || threadRuntime.session == nil {
		return nil, errors.New("thread request is unavailable")
	}
	return threadRuntime.session.Request(ctx, request)
}

func (threadRuntime *AmadeusThread) Shutdown(ctx context.Context) error {
	if threadRuntime == nil {
		return nil
	}
	if err := threadRuntime.Submit(ctx, protocol.ShutdownOp{}); err != nil {
		select {
		case <-threadRuntime.io.Terminated:
			return nil
		default:
			return err
		}
	}
	select {
	case <-threadRuntime.io.Terminated:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (threadRuntime *AmadeusThread) sessionRename(ctx context.Context, title string, at time.Time) error {
	if threadRuntime == nil || threadRuntime.session == nil {
		return fmt.Errorf("thread %q is unavailable", threadRuntime.id)
	}
	return threadRuntime.session.Rename(ctx, title, at)
}
