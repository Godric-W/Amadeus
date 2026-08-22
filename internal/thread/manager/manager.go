package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
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
	store     thread.ThreadStore
	services  SharedServices
	threads   map[protocol.ThreadID]*AmadeusThread
	closed    bool
}

type AmadeusThread struct {
	manager          *ThreadManager
	id               protocol.ThreadID
	live             *thread.LiveThread
	session          *agentsession.Session
	io               agentsession.SessionIo
	nextID           func(string) string
	agentControl     *multiagent.Control
	ownsAgentControl bool
}

func New(ctx context.Context, store thread.ThreadStore, services SharedServices) (*ThreadManager, error) {
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
	id := protocol.ThreadID(manager.services.NextID("thread"))
	control, err := multiagent.NewControl(id, manager, multiagent.Options{
		MaxAgents: input.Configuration.Runtime.Agent.MultiAgent.MaxAgents,
		MaxDepth:  input.Configuration.Runtime.Agent.MultiAgent.MaxDepth,
	})
	if err != nil {
		return nil, err
	}
	input.Configuration.Source = protocol.RootSessionSource()
	live, err := thread.NewDraftLiveThread(id, manager.store)
	if err != nil {
		return nil, err
	}
	return manager.spawn(ctx, id, live, thread.InitialHistory{Kind: thread.InitialHistoryNew}, input, control, true)
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
	meta, ok := history.Lines[0].Item.(rollout.SessionMetaItem)
	if !ok {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("resumed thread does not begin with session metadata")
	}
	if meta.Source.IsSubAgent() {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("sub-agent threads cannot be resumed directly")
	}
	control, err := multiagent.NewControl(id, manager, multiagent.Options{
		MaxAgents: input.Configuration.Runtime.Agent.MultiAgent.MaxAgents,
		MaxDepth:  input.Configuration.Runtime.Agent.MultiAgent.MaxDepth,
	})
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	input.Configuration.Source = protocol.RootSessionSource()
	return manager.spawn(ctx, id, live, history, input, control, true)
}

func (manager *ThreadManager) spawn(ctx context.Context, id protocol.ThreadID, live *thread.LiveThread, history thread.InitialHistory, input StartInput, control *multiagent.Control, ownsControl bool) (*AmadeusThread, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !manager.services.SessionAdapters.ModelMessages.HasInstructions() {
		_ = live.Shutdown(context.Background())
		return nil, errors.New("thread session services are unavailable")
	}
	session, io, err := agentsession.Spawn(manager.ctx, agentsession.SpawnArgs{
		ThreadID: id, History: history,
		State:    agentsession.SessionState{Configuration: input.Configuration},
		Services: agentsession.SessionServices{LiveThread: live, Clock: manager.services.Clock, NextID: manager.services.NextID, AgentControl: control},
		Adapters: manager.services.SessionAdapters,
	})
	if err != nil {
		_ = live.Shutdown(context.Background())
		return nil, err
	}
	value := &AmadeusThread{
		manager: manager, id: id, live: live, session: session, io: io, nextID: manager.services.NextID,
		agentControl: control, ownsAgentControl: ownsControl,
	}
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

func (manager *ThreadManager) ListThreads(ctx context.Context, query state.ListQuery) ([]state.StoredThread, error) {
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

func (manager *ThreadManager) SpawnChild(ctx context.Context, control *multiagent.Control, request multiagent.SpawnChildRequest) (multiagent.AgentRuntime, error) {
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.closed {
		return nil, errors.New("thread manager is closed")
	}
	if control == nil || control.RootID() == "" {
		return nil, errors.New("child agent control is unavailable")
	}
	manager.mu.Lock()
	parent := manager.threads[request.ParentThreadID]
	manager.mu.Unlock()
	if parent == nil || parent.agentControl != control {
		return nil, fmt.Errorf("parent thread %q is unavailable", request.ParentThreadID)
	}
	id := protocol.ThreadID(manager.services.NextID("thread"))
	configuration := parent.session.Configuration()
	configuration.Source = protocol.NewSubAgentSessionSource(request.ParentThreadID, request.Depth, request.Nickname, request.Role)
	configuration.Mode = turn.ModeKindDefault
	live, err := thread.NewDraftLiveThread(id, manager.store)
	if err != nil {
		return nil, err
	}
	child, err := manager.spawn(ctx, id, live, thread.InitialHistory{Kind: thread.InitialHistoryNew}, StartInput{Configuration: configuration}, control, false)
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
	statusPayload := map[string]string{string(notification.Status.Kind): notification.Status.Message}
	if notification.Status.Message == "" {
		statusPayload[string(notification.Status.Kind)] = ""
	}
	payload, err := json.Marshal(map[string]any{
		"agent_id": notification.Metadata.ThreadID,
		"nickname": notification.Metadata.AgentNickname,
		"status":   statusPayload,
	})
	if err != nil {
		return err
	}
	content := "<subagent_notification>\n" + string(payload) + "\n</subagent_notification>"
	return parent.session.AppendSubagentNotification(ctx, notification.Metadata.ThreadID, content)
}

func (threadRuntime *AmadeusThread) ID() protocol.ThreadID {
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

func (threadRuntime *AmadeusThread) History(ctx context.Context) ([]rollout.Line, error) {
	if threadRuntime == nil || threadRuntime.live == nil {
		return nil, errors.New("thread history is unavailable")
	}
	history, err := threadRuntime.live.History(ctx)
	if err != nil {
		return nil, err
	}
	return rollout.CloneLines(history.Lines), nil
}

func (threadRuntime *AmadeusThread) RolloutItemCount() int {
	if threadRuntime == nil || threadRuntime.session == nil {
		return 0
	}
	return threadRuntime.session.RolloutItemCount()
}

func (threadRuntime *AmadeusThread) Configuration() protocol.SessionConfiguration {
	if threadRuntime == nil || threadRuntime.session == nil {
		return protocol.SessionConfiguration{}
	}
	return threadRuntime.session.ProtocolConfiguration()
}

func (threadRuntime *AmadeusThread) ContextWindow() int64 {
	if threadRuntime == nil || threadRuntime.session == nil {
		return 0
	}
	return threadRuntime.session.Configuration().Runtime.ModelContextWindow
}

func (threadRuntime *AmadeusThread) Submit(ctx context.Context, op protocol.Op) error {
	if threadRuntime == nil || op == nil || threadRuntime.nextID == nil {
		return errors.New("thread submission is empty")
	}
	submission := protocol.Submission{ID: protocol.SubmissionID(threadRuntime.nextID("submission")), Op: op}
	select {
	case threadRuntime.io.Submissions <- submission:
		return nil
	case <-threadRuntime.io.Terminated:
		return errors.New("thread is terminated")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (threadRuntime *AmadeusThread) SubmitUserInputAndWaitForAdmission(ctx context.Context, op protocol.UserInputOp) (protocol.UserMessageAdmission, error) {
	if threadRuntime == nil || threadRuntime.nextID == nil {
		return protocol.UserMessageAdmission{}, errors.New("thread user message submission is unavailable")
	}
	if op.ClientUserMessageID == "" {
		op.ClientUserMessageID = threadRuntime.nextID("user-message")
	}
	submission := protocol.Submission{ID: protocol.SubmissionID(threadRuntime.nextID("submission")), Op: op}
	return threadRuntime.io.SubmitUserInputAndWaitForAdmission(ctx, submission)
}

func (threadRuntime *AmadeusThread) SubmitUserInput(ctx context.Context, op protocol.UserInputOp) error {
	_, err := threadRuntime.SubmitUserInputAndWaitForAdmission(ctx, op)
	return err
}

func (threadRuntime *AmadeusThread) Events() <-chan protocol.Event {
	if threadRuntime == nil {
		return nil
	}
	return threadRuntime.io.Events
}

func (threadRuntime *AmadeusThread) Terminated() <-chan struct{} {
	if threadRuntime == nil {
		return nil
	}
	return threadRuntime.io.Terminated
}

func (threadRuntime *AmadeusThread) SteerInput(ctx context.Context, expectedTurnID protocol.TurnID, content, clientUserMessageID string) (protocol.TurnID, error) {
	if threadRuntime == nil {
		return "", errors.New("thread steer input is unavailable")
	}
	if clientUserMessageID == "" && threadRuntime.nextID != nil {
		clientUserMessageID = threadRuntime.nextID("user-message")
	}
	return threadRuntime.io.SteerInput(ctx, expectedTurnID, agentsession.UserTurnInput{Content: content, ClientID: clientUserMessageID})
}

func (threadRuntime *AmadeusThread) Shutdown(ctx context.Context) error {
	if threadRuntime == nil {
		return nil
	}
	var result error
	if threadRuntime.ownsAgentControl && threadRuntime.agentControl != nil {
		result = threadRuntime.agentControl.Close(ctx)
	}
	if err := threadRuntime.Submit(ctx, protocol.ShutdownOp{}); err != nil {
		select {
		case <-threadRuntime.io.Terminated:
			return result
		default:
			return errors.Join(result, err)
		}
	}
	select {
	case <-threadRuntime.io.Terminated:
		if threadRuntime.manager != nil {
			threadRuntime.manager.removeThread(threadRuntime.id, threadRuntime)
		}
		return result
	case <-ctx.Done():
		return errors.Join(result, ctx.Err())
	}
}

func (threadRuntime *AmadeusThread) sessionRename(ctx context.Context, title string, at time.Time) error {
	if threadRuntime == nil || threadRuntime.session == nil {
		return fmt.Errorf("thread %q is unavailable", threadRuntime.id)
	}
	return threadRuntime.session.Rename(ctx, title, at)
}
