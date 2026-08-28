package threadmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type SharedServices struct {
	SessionAdapters agentsession.ServiceAdapters
	Clock           func() time.Time
	NextID          func(string) string
}

type StartInput struct {
	Configuration agentsession.Configuration
}

type ThreadManager struct {
	lifecycle sync.RWMutex
	mu        sync.Mutex
	ctx       context.Context
	store     threadstore.ThreadStore
	services  SharedServices
	threads   map[protocol.ThreadID]*AmadeusThread
	closed    bool
}

func New(ctx context.Context, store threadstore.ThreadStore, services SharedServices) (*ThreadManager, error) {
	if ctx == nil || store == nil || services.NextID == nil {
		return nil, errors.New("thread manager is incomplete")
	}
	if services.Clock == nil {
		services.Clock = time.Now
	}
	return &ThreadManager{ctx: ctx, store: store, services: services, threads: make(map[protocol.ThreadID]*AmadeusThread)}, nil
}

func (manager *ThreadManager) StartThread(ctx context.Context, input StartInput) (*AmadeusThread, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	id, err := protocol.NewThreadID()
	if err != nil {
		return nil, err
	}
	sessionID := protocol.SessionIDFromThreadID(id)
	control, err := multiagent.NewControl(sessionID, id, manager, multiagent.Options{
		MaxAgents: input.Configuration.Runtime.Agent.MultiAgent.MaxAgents,
		MaxDepth:  input.Configuration.Runtime.Agent.MultiAgent.MaxDepth,
	})
	if err != nil {
		return nil, err
	}
	input.Configuration.Source = protocol.RootSessionSource()
	live, err := threadstore.NewDraftLiveThread(id, manager.store)
	if err != nil {
		return nil, err
	}
	return manager.spawn(ctx, sessionID, id, nil, live, threadstore.InitialHistory{Kind: threadstore.InitialHistoryNew}, input, control, true)
}

func (manager *ThreadManager) ResumeThread(ctx context.Context, id protocol.ThreadID, input StartInput) (*AmadeusThread, error) {
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
	stored, err := manager.store.GetThread(ctx, id)
	if err != nil {
		return nil, err
	}
	if stored.ID != id {
		return nil, errors.New("stored thread identity does not match resume target")
	}
	if stored.Source.IsSubAgent() {
		return nil, errors.New("sub-agent threads cannot be resumed directly")
	}
	live, history, err := threadstore.NewResumedLiveThread(ctx, id, manager.store)
	if err != nil {
		return nil, err
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	history, err = threadstore.RecoverInterruptedTurn(recoveryCtx, live, history)
	cancel()
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	meta, ok := history.Lines[0].Item.(rollout.SessionMetaItem)
	if !ok {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("resumed thread does not begin with session metadata")
	}
	if meta.Source.IsSubAgent() {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("sub-agent threads cannot be resumed directly")
	}
	if meta.ID != id || meta.SessionID != protocol.SessionIDFromThreadID(id) || meta.ParentThreadID != nil {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("root session metadata identity does not match resume target")
	}
	control, err := multiagent.NewControl(meta.SessionID, id, manager, multiagent.Options{
		MaxAgents: input.Configuration.Runtime.Agent.MultiAgent.MaxAgents,
		MaxDepth:  input.Configuration.Runtime.Agent.MultiAgent.MaxDepth,
	})
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	input.Configuration.Source = protocol.RootSessionSource()
	root, err := manager.spawn(ctx, meta.SessionID, id, nil, live, history, input, control, true)
	if err != nil {
		return nil, err
	}
	if err := manager.restorePersistedChildren(ctx, root, control); err != nil {
		_ = root.Shutdown(context.Background())
		return nil, err
	}
	return root, nil
}

func (manager *ThreadManager) spawn(ctx context.Context, sessionID protocol.SessionID, id protocol.ThreadID, parentThreadID *protocol.ThreadID, live *threadstore.LiveThread, history threadstore.InitialHistory, input StartInput, control *multiagent.Control, ownsControl bool) (*AmadeusThread, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !manager.services.SessionAdapters.ModelMessages.HasInstructions() {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("thread session services are unavailable")
	}
	session, io, err := agentsession.Spawn(manager.ctx, agentsession.SpawnArgs{
		SessionID: sessionID, ThreadID: id, ParentThreadID: cloneThreadID(parentThreadID), History: history,
		State:    agentsession.SessionState{Configuration: input.Configuration},
		Services: agentsession.SessionServices{LiveThread: live, Clock: manager.services.Clock, NextID: manager.services.NextID, AgentControl: control},
		Adapters: manager.services.SessionAdapters,
	})
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	value := &AmadeusThread{
		manager: manager, sessionID: sessionID, id: id, parentThreadID: cloneThreadID(parentThreadID), live: live, session: session, io: io, nextID: manager.services.NextID,
		agentControl: control, ownsAgentControl: ownsControl,
	}
	select {
	case configuredErr, ok := <-io.Configured:
		if !ok || configuredErr != nil {
			_ = value.Shutdown(context.Background())
			if configuredErr == nil {
				configuredErr = errors.New("session terminated before configuration completed")
			}
			return nil, configuredErr
		}
	case <-io.Terminated:
		return nil, errors.New("session terminated before configuration completed")
	case <-ctx.Done():
		_ = value.Shutdown(context.Background())
		return nil, ctx.Err()
	}
	manager.mu.Lock()
	if existing := manager.threads[id]; existing != nil {
		manager.mu.Unlock()
		_ = value.Shutdown(context.Background())
		return nil, fmt.Errorf("thread %q is already registered", id)
	}
	manager.threads[id] = value
	manager.mu.Unlock()
	go func() {
		<-io.Terminated
		if value.ownsAgentControl && value.agentControl != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = value.agentControl.Close(cleanupCtx)
			cancel()
		}
		manager.removeThread(id, value)
	}()
	return value, nil
}

func (manager *ThreadManager) GetThread(id protocol.ThreadID) (*AmadeusThread, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	value, ok := manager.threads[id]
	return value, ok
}

func (manager *ThreadManager) ListThreads(ctx context.Context, query threadstore.ListQuery) ([]threadstore.StoredThread, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	return manager.store.ListThreads(ctx, query)
}

func (manager *ThreadManager) RenameThread(ctx context.Context, id protocol.ThreadID, title string) error {
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

func (manager *ThreadManager) DeleteThread(ctx context.Context, id protocol.ThreadID) error {
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

func (manager *ThreadManager) ShutdownThread(ctx context.Context, id protocol.ThreadID) error {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil
	}
	value, ok := manager.GetThread(id)
	if !ok {
		return nil
	}
	if err := value.Shutdown(ctx); err != nil {
		return err
	}
	manager.removeThread(id, value)
	return nil
}

func (manager *ThreadManager) removeThread(id protocol.ThreadID, expected *AmadeusThread) {
	manager.mu.Lock()
	if manager.threads[id] == expected {
		delete(manager.threads, id)
	}
	manager.mu.Unlock()
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
