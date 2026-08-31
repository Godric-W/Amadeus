package threadmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	lifecycle   sync.RWMutex
	mu          sync.Mutex
	ctx         context.Context
	store       threadstore.ThreadStore
	services    SharedServices
	threads     map[protocol.ThreadID]*AmadeusThread
	closed      bool
	storeClosed bool
}

// ThreadShutdownResult records the outcome of one independent Thread shutdown.
// A timed-out or failed Thread remains in the manager registry so its owner can
// inspect it or retry cleanup; it is never reported as completed by omission.
type ThreadShutdownResult struct {
	ThreadID protocol.ThreadID
	Outcome  ThreadShutdownOutcome
	Err      error
}

type ThreadShutdownOutcome string

const (
	ThreadShutdownCompleted  ThreadShutdownOutcome = "completed"
	ThreadShutdownSubmitFail ThreadShutdownOutcome = "submit_failed"
	ThreadShutdownTimedOut   ThreadShutdownOutcome = "timed_out"
)

type ThreadShutdownReport struct {
	Results []ThreadShutdownResult
}

func (report ThreadShutdownReport) Completed() []protocol.ThreadID {
	return report.ids(ThreadShutdownCompleted)
}

func (report ThreadShutdownReport) SubmitFailed() []protocol.ThreadID {
	return report.ids(ThreadShutdownSubmitFail)
}

func (report ThreadShutdownReport) TimedOut() []protocol.ThreadID {
	return report.ids(ThreadShutdownTimedOut)
}

func (report ThreadShutdownReport) ids(outcome ThreadShutdownOutcome) []protocol.ThreadID {
	ids := make([]protocol.ThreadID, 0)
	for _, result := range report.Results {
		if result.Outcome == outcome {
			ids = append(ids, result.ThreadID)
		}
	}
	return ids
}

func (report ThreadShutdownReport) Err() error {
	var result error
	for _, shutdown := range report.Results {
		if shutdown.Err != nil {
			result = errors.Join(result, fmt.Errorf("thread %s shutdown: %w", shutdown.ThreadID.String(), shutdown.Err))
		}
	}
	return result
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
		_ = live.Discard(context.Background())
		return nil, err
	}
	meta, ok := history.Lines[0].Item.(rollout.SessionMetaItem)
	if !ok {
		_ = live.Discard(context.Background())
		return nil, errors.New("resumed thread does not begin with session metadata")
	}
	if meta.Source.IsSubAgent() {
		_ = live.Discard(context.Background())
		return nil, errors.New("sub-agent threads cannot be resumed directly")
	}
	if meta.ID != id || meta.SessionID != protocol.SessionIDFromThreadID(id) || meta.ParentThreadID != nil {
		_ = live.Discard(context.Background())
		return nil, errors.New("root session metadata identity does not match resume target")
	}
	control, err := multiagent.NewControl(meta.SessionID, id, manager, multiagent.Options{
		MaxAgents: input.Configuration.Runtime.Agent.MultiAgent.MaxAgents,
		MaxDepth:  input.Configuration.Runtime.Agent.MultiAgent.MaxDepth,
	})
	if err != nil {
		_ = live.Discard(context.Background())
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
		_ = live.Discard(context.Background())
		return nil, errors.New("thread session services are unavailable")
	}
	session, io, err := agentsession.Spawn(manager.ctx, agentsession.SpawnArgs{
		SessionID: sessionID, ThreadID: id, ParentThreadID: cloneThreadID(parentThreadID), History: history,
		State:    agentsession.SessionState{Configuration: input.Configuration},
		Services: agentsession.SessionServices{LiveThread: live, Clock: manager.services.Clock, NextID: manager.services.NextID, AgentControl: control},
		Adapters: manager.services.SessionAdapters,
	})
	if err != nil {
		_ = live.Discard(context.Background())
		return nil, err
	}
	value := &AmadeusThread{
		manager: manager, sessionID: sessionID, id: id, parentThreadID: cloneThreadID(parentThreadID), live: live, session: session, io: io, nextID: manager.services.NextID,
		agentControl: control, ownsAgentControl: ownsControl,
	}
	select {
	case configuredErr, ok := <-io.Configured:
		if !ok || configuredErr != nil {
			manager.abortSession(session, control, ownsControl, configuredErr)
			if configuredErr == nil {
				configuredErr = errors.New("session terminated before configuration completed")
			}
			return nil, configuredErr
		}
	case <-io.Terminated:
		if ownsControl && control != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = control.Close(cleanupCtx)
			cancel()
		}
		return nil, errors.New("session terminated before configuration completed")
	case <-ctx.Done():
		manager.abortSession(session, control, ownsControl, ctx.Err())
		return nil, ctx.Err()
	}
	manager.mu.Lock()
	if existing := manager.threads[id]; existing != nil {
		manager.mu.Unlock()
		manager.abortSession(session, control, ownsControl, fmt.Errorf("thread %q is already registered", id))
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

func (manager *ThreadManager) abortSession(session *agentsession.Session, control *multiagent.Control, ownsControl bool, cause error) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = session.AbortInitialization(cleanupCtx, cause)
	if ownsControl && control != nil {
		_ = control.Close(cleanupCtx)
	}
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
	if ctx == nil {
		return errors.New("thread manager close context is nil")
	}
	manager.lifecycle.Lock()
	if manager.closed {
		manager.lifecycle.Unlock()
		// A previous bounded close may have left timed-out Threads tracked. The
		// caller can retry with a fresh context instead of receiving a false
		// success, while a fully closed Store remains idempotent.
		return manager.closeRemaining(ctx)
	}
	manager.closed = true
	manager.lifecycle.Unlock()
	return manager.closeRemaining(ctx)
}

func (manager *ThreadManager) closeRemaining(ctx context.Context) error {
	report := manager.ShutdownAllThreadsBounded(ctx, shutdownTimeoutFromContext(ctx))
	result := report.Err()
	if len(report.SubmitFailed()) != 0 || len(report.TimedOut()) != 0 {
		// Keep the Store open while any Thread may still own a writer. Closing
		// it here would turn a timeout into an irreversible, silent teardown.
		return result
	}
	manager.lifecycle.Lock()
	if manager.storeClosed {
		manager.lifecycle.Unlock()
		return result
	}
	storeErr := manager.store.Close()
	if storeErr == nil {
		manager.storeClosed = true
	}
	manager.lifecycle.Unlock()
	return errors.Join(result, storeErr)
}

const defaultThreadShutdownTimeout = 5 * time.Second

func shutdownTimeoutFromContext(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			return remaining
		}
		return time.Nanosecond
	}
	return defaultThreadShutdownTimeout
}

// ShutdownAllThreadsBounded concurrently asks every tracked Thread to stop and
// waits at most timeout for each one. The result is stable by ThreadID and only
// completed Threads are removed from the registry.
func (manager *ThreadManager) ShutdownAllThreadsBounded(ctx context.Context, timeout time.Duration) ThreadShutdownReport {
	if manager == nil {
		return ThreadShutdownReport{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultThreadShutdownTimeout
	}
	manager.mu.Lock()
	threads := make(map[protocol.ThreadID]*AmadeusThread, len(manager.threads))
	for id, value := range manager.threads {
		threads[id] = value
	}
	manager.mu.Unlock()

	results := make(chan ThreadShutdownResult, len(threads))
	var wait sync.WaitGroup
	for id, value := range threads {
		wait.Add(1)
		go func(id protocol.ThreadID, value *AmadeusThread) {
			defer wait.Done()
			threadCtx, cancel := context.WithTimeout(ctx, timeout)
			err := value.Shutdown(threadCtx)
			outcome := classifyThreadShutdown(threadCtx, err)
			cancel()
			results <- ThreadShutdownResult{ThreadID: id, Outcome: outcome, Err: err}
		}(id, value)
	}
	wait.Wait()
	close(results)
	report := ThreadShutdownReport{Results: make([]ThreadShutdownResult, 0, len(threads))}
	manager.mu.Lock()
	for result := range results {
		report.Results = append(report.Results, result)
		if result.Outcome == ThreadShutdownCompleted {
			delete(manager.threads, result.ThreadID)
		}
	}
	manager.mu.Unlock()
	sort.Slice(report.Results, func(i, j int) bool {
		return report.Results[i].ThreadID.String() < report.Results[j].ThreadID.String()
	})
	return report
}

func classifyThreadShutdown(ctx context.Context, err error) ThreadShutdownOutcome {
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ThreadShutdownTimedOut
	}
	if err == nil {
		return ThreadShutdownCompleted
	}
	return ThreadShutdownSubmitFail
}
