package manager

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/Godric-W/Amadeus/internal/thread/local"
)

type testFactory struct {
	block bool
}

type concurrentHistoryFactory struct {
	observed chan int
}

type terminalFailStore struct {
	thread.ThreadStore
}

type materializeFailStore struct {
	thread.ThreadStore
}

type turnStartFailStore struct {
	thread.ThreadStore
}

type terminalModeFactory struct{ mode string }

type abortTrackingFactory struct{ aborts *atomic.Int32 }

type testTaskSource interface {
	NewTask(context.Context, *session.Session, session.TaskKind, string, turn.TurnContext) (session.SessionTask, turn.TurnContext, error)
	Close() error
}

func taskConstructors(source testTaskSource) session.TaskConstructors {
	return session.TaskConstructors{
		Regular: func(ctx context.Context, active *session.Session, input string, value turn.TurnContext) (session.SessionTask, turn.TurnContext, error) {
			return source.NewTask(ctx, active, session.TaskKindRegular, input, value)
		},
		Compact: func(ctx context.Context, active *session.Session, input string, value turn.TurnContext) (session.SessionTask, turn.TurnContext, error) {
			return source.NewTask(ctx, active, session.TaskKindCompact, input, value)
		},
		Close: source.Close,
	}
}

func (factory testFactory) NewTask(_ context.Context, _ *session.Session, kind session.TaskKind, _ string, value turn.TurnContext) (session.SessionTask, turn.TurnContext, error) {
	sessionTask := session.FuncTask{TaskKind: kind, RunFunc: func(ctx context.Context, _ *session.Session, _ *turn.TurnContext, _ []session.TurnInput) (session.Result, error) {
		if factory.block {
			<-ctx.Done()
			return session.Result{}, ctx.Err()
		}
		item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "done"})
		return session.Result{Items: []rollout.Item{item}}, err
	}}
	return sessionTask, value, nil
}

func (testFactory) Close() error { return nil }

func (factory concurrentHistoryFactory) NewTask(_ context.Context, active *session.Session, kind session.TaskKind, _ string, value turn.TurnContext) (session.SessionTask, turn.TurnContext, error) {
	sessionTask := session.FuncTask{TaskKind: kind, RunFunc: func(ctx context.Context, host *session.Session, turnContext *turn.TurnContext, _ []session.TurnInput) (session.Result, error) {
		var wait sync.WaitGroup
		errorsChannel := make(chan error, 16)
		for index := range 16 {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "parallel"})
				if err == nil {
					err = host.AppendItems(ctx, turnContext.TurnID, item)
				}
				errorsChannel <- err
			}(index)
		}
		wait.Wait()
		close(errorsChannel)
		for err := range errorsChannel {
			if err != nil {
				return session.Result{}, err
			}
		}
		factory.observed <- len(host.History())
		return session.Result{}, nil
	}}
	_ = active
	return sessionTask, value, nil
}

func (concurrentHistoryFactory) Close() error { return nil }

func (store terminalFailStore) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.Item) (thread.AppendResult, error) {
	for _, item := range items {
		if item.Kind == rollout.KindTurnCompleted || item.Kind == rollout.KindTurnAborted {
			return thread.AppendResult{}, errors.New("terminal persistence failed")
		}
	}
	return store.ThreadStore.AppendItems(ctx, id, turnID, items...)
}

func (store materializeFailStore) Materialize(context.Context, thread.CreateInput) (thread.AppendResult, error) {
	return thread.AppendResult{}, errors.New("materialize failed")
}

func (store turnStartFailStore) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.Item) (thread.AppendResult, error) {
	for _, item := range items {
		if item.Kind == rollout.KindTurnContext || item.Kind == rollout.KindTurnStarted {
			return thread.AppendResult{}, errors.New("turn start append failed")
		}
	}
	return store.ThreadStore.AppendItems(ctx, id, turnID, items...)
}

func (factory terminalModeFactory) NewTask(_ context.Context, _ *session.Session, kind session.TaskKind, _ string, value turn.TurnContext) (session.SessionTask, turn.TurnContext, error) {
	valueTask := session.FuncTask{TaskKind: kind, RunFunc: func(context.Context, *session.Session, *turn.TurnContext, []session.TurnInput) (session.Result, error) {
		switch factory.mode {
		case "failed":
			return session.Result{Summary: "failed summary"}, errors.New("task failed")
		case "panic":
			panic("task panic")
		default:
			return session.Result{Summary: "completed summary"}, nil
		}
	}}
	return valueTask, value, nil
}

func (terminalModeFactory) Close() error { return nil }

func (factory abortTrackingFactory) NewTask(_ context.Context, _ *session.Session, kind session.TaskKind, _ string, value turn.TurnContext) (session.SessionTask, turn.TurnContext, error) {
	taskValue := session.FuncTask{
		TaskKind: kind,
		RunFunc: func(context.Context, *session.Session, *turn.TurnContext, []session.TurnInput) (session.Result, error) {
			return session.Result{}, errors.New("task must not run")
		},
		AbortFunc: func(context.Context, *session.Session, *turn.TurnContext) error {
			factory.aborts.Add(1)
			return nil
		},
	}
	return taskValue, value, nil
}

func (abortTrackingFactory) Close() error { return nil }

func TestThreadManagerMaterializesOnFirstInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, store := newTestManager(t, ctx, testFactory{})
	defer manager.Close(context.Background())
	configuration := testConfiguration(t)
	value, err := manager.StartThread(ctx, StartInput{Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := manager.ListThreads(ctx, state.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("draft thread was persisted: %#v", listed)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "inspect files"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), false)
	listed, err = manager.ListThreads(ctx, state.ListQuery{CWD: configuration.CWD})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != value.ID() || listed[0].Preview != "inspect files" {
		t.Fatalf("threads = %#v", listed)
	}
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []rollout.Kind{
		rollout.KindSessionMeta, rollout.KindTurnContext, rollout.KindResponseItem,
		rollout.KindTurnStarted, rollout.KindResponseItem, rollout.KindTurnCompleted,
	}
	if len(history.Lines) != len(wantKinds) {
		t.Fatalf("history lines = %d, want %d", len(history.Lines), len(wantKinds))
	}
	for index, kind := range wantKinds {
		if history.Lines[index].Item.Kind != kind {
			t.Fatalf("history kind[%d] = %q, want %q", index, history.Lines[index].Item.Kind, kind)
		}
	}
}

func TestThreadManagerInterruptPersistsAbortedBeforeEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, store := newTestManager(t, ctx, testFactory{block: true})
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "wait"}); err != nil {
		t.Fatal(err)
	}
	waitForStarted(t, value.Io())
	if err := value.Submit(ctx, protocol.InterruptOp{}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), true)
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	if history.Lines[len(history.Lines)-1].Item.Kind != rollout.KindTurnAborted {
		t.Fatalf("last rollout item = %q", history.Lines[len(history.Lines)-1].Item.Kind)
	}
}

func TestSessionHistorySupportsConcurrentTaskAppends(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := make(chan int, 1)
	manager, _ := newTestManager(t, ctx, concurrentHistoryFactory{observed: observed})
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "parallel history"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), false)
	if count := <-observed; count != 20 {
		t.Fatalf("history count before terminal = %d, want 20", count)
	}
}

func TestRenameActiveThreadUpdatesCanonicalSessionHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, _ := newTestManager(t, ctx, testFactory{})
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "materialize"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), false)
	if err := manager.RenameThread(ctx, value.ID(), "Renamed Thread"); err != nil {
		t.Fatal(err)
	}
	history := value.History()
	if len(history) == 0 || history[len(history)-1].Item.Kind != rollout.KindContextUpdate {
		t.Fatalf("history after rename = %#v", history)
	}
	update, err := rollout.DecodePayload[rollout.ContextUpdate](history[len(history)-1].Item)
	if err != nil {
		t.Fatal(err)
	}
	if update.Title != "Renamed Thread" {
		t.Fatalf("rename update = %#v", update)
	}
}

func TestTerminalPersistenceFailureDoesNotPublishTerminalEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, baseStore := newTestManager(t, ctx, testFactory{})
	manager.store = terminalFailStore{ThreadStore: baseStore}
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "fail terminal"}); err != nil {
		t.Fatal(err)
	}
	var streamError bool
	for event := range value.Io().Events {
		switch event.Msg.(type) {
		case protocol.StreamErrorEvent:
			streamError = true
		case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
			t.Fatal("terminal event was published without durable terminal rollout")
		}
	}
	if !streamError {
		t.Fatal("terminal persistence failure was not reported")
	}
}

func TestTurnStartFailurePublishesRejectedWithoutBlockingSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, baseStore := newTestManager(t, ctx, testFactory{})
	manager.store = materializeFailStore{ThreadStore: baseStore}
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := value.Submit(ctx, protocol.UserInputOp{Content: "retry"}); err != nil {
			t.Fatal(err)
		}
		timeout := time.After(5 * time.Second)
		for {
			select {
			case event := <-value.Io().Events:
				if rejected, ok := event.Msg.(protocol.ErrorEvent); ok {
					if rejected.Message != "materialize failed" {
						t.Fatalf("rejected = %#v", rejected)
					}
					goto nextAttempt
				}
			case <-timeout:
				t.Fatal("timed out waiting for ErrorEvent")
			}
		}
	nextAttempt:
	}
}

func TestSessionPublishesAndPersistsExactlyOneTerminal(t *testing.T) {
	for _, mode := range []string{"completed", "failed", "panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			manager, store := newTestManager(t, ctx, terminalModeFactory{mode: mode})
			defer manager.Close(context.Background())
			value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
			if err != nil {
				t.Fatal(err)
			}
			if err := value.Submit(ctx, protocol.UserInputOp{Content: "run"}); err != nil {
				t.Fatal(err)
			}
			terminalEvents := 0
			timeout := time.After(5 * time.Second)
			for terminalEvents == 0 {
				select {
				case event := <-value.Io().Events:
					switch event.Msg.(type) {
					case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
						terminalEvents++
					}
				case <-timeout:
					t.Fatal("timed out waiting for terminal")
				}
			}
			if err := value.Submit(ctx, protocol.ShutdownOp{}); err != nil {
				t.Fatal(err)
			}
			for event := range value.Io().Events {
				switch event.Msg.(type) {
				case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
					terminalEvents++
				}
			}
			if terminalEvents != 1 {
				t.Fatalf("terminal event count = %d", terminalEvents)
			}
			history, err := store.LoadHistory(ctx, value.ID())
			if err != nil {
				t.Fatal(err)
			}
			terminalLines := 0
			for _, line := range history.Lines {
				if line.Item.Kind == rollout.KindTurnCompleted || line.Item.Kind == rollout.KindTurnAborted {
					terminalLines++
				}
			}
			if terminalLines != 1 {
				t.Fatalf("terminal rollout count = %d", terminalLines)
			}
		})
	}
}

func TestPreparedTaskAbortsWhenDurableTurnStartFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var aborts atomic.Int32
	manager, baseStore := newTestManager(t, ctx, abortTrackingFactory{aborts: &aborts})
	manager.store = turnStartFailStore{ThreadStore: baseStore}
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "fail durable start"}); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-value.Io().Events:
			if rejected, ok := event.Msg.(protocol.ErrorEvent); ok {
				if rejected.Message != "turn start append failed" {
					t.Fatalf("rejected = %#v", rejected)
				}
				if aborts.Load() != 1 {
					t.Fatalf("prepared task abort count = %d", aborts.Load())
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for rejected turn")
		}
	}
}

func TestResumeRecoversIncompleteToolCall(t *testing.T) {
	ctx := context.Background()
	manager, store := newTestManager(t, ctx, testFactory{})
	now := time.Now().UTC()
	live, err := thread.NewDraftLiveThread("thread-recover", store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.Materialize(ctx, thread.CreateInput{CWD: testConfiguration(t).CWD, Title: "recover", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	turnContext := testTurnContext(t, "thread-recover", "turn-old")
	contextItem, _ := rollout.NewItem(rollout.KindTurnContext, turnContext)
	userItem, _ := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "run"})
	startedItem, _ := rollout.NewItem(rollout.KindTurnStarted, rollout.TurnStarted{Input: "run"})
	toolCall, _ := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "execute_command", Arguments: json.RawMessage(`{}`)})
	if _, err := live.AppendItems(ctx, "turn-old", contextItem, userItem, startedItem, toolCall); err != nil {
		t.Fatal(err)
	}
	if err := live.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	resumed, err := manager.ResumeThread(ctx, "thread-recover", StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Shutdown(context.Background())
	history, err := store.LoadHistory(ctx, "thread-recover")
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Lines) < 2 || history.Lines[len(history.Lines)-1].Item.Kind != rollout.KindTurnAborted {
		t.Fatalf("recovered history = %#v", history.Lines)
	}
	var recovered struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(history.Lines[len(history.Lines)-2].Item.Payload, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Type != "tool_result" || recovered.CallID != "call-1" || recovered.Status != "cancelled" {
		t.Fatalf("recovered result = %#v", recovered)
	}
}

func newTestManager(t *testing.T, ctx context.Context, factory testTaskSource) (*ThreadManager, thread.ThreadStore) {
	t.Helper()
	home := t.TempDir()
	database, err := statesqlite.Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	stateStore, err := statesqlite.NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	localStore, err := local.NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sequence atomic.Uint64
	manager, err := New(ctx, localStore, SharedServices{
		DefaultSessionSetup: session.SessionSetup{TaskConstructors: taskConstructors(factory)},
		NextID: func(prefix string) string {
			return prefix + "-" + time.Unix(0, int64(sequence.Add(1))).UTC().Format("150405.000000000")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager, localStore
}

func testConfiguration(t *testing.T) session.Configuration {
	t.Helper()
	return session.Configuration{
		CWD: filepath.Clean(t.TempDir()), Provider: "openai", Model: "gpt-test",
		Mode: turn.ModeKindDefault,
	}
}

func testTurnContext(t *testing.T, threadID protocol.ThreadID, turnID protocol.TurnID) turn.TurnContext {
	t.Helper()
	configuration := testConfiguration(t)
	return turn.TurnContext{
		ThreadID: threadID, TurnID: turnID, Provider: configuration.Provider, Model: configuration.Model,
		CWD: configuration.CWD, Mode: turn.ModeKindDefault,
	}
}

func waitForStarted(t *testing.T, io session.SessionIo) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-io.Events:
			if _, ok := event.Msg.(protocol.TurnStartedEvent); ok {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for TurnStarted")
		}
	}
}

func waitForTerminal(t *testing.T, io session.SessionIo, aborted bool) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-io.Events:
			switch event.Msg.(type) {
			case protocol.TurnCompleteEvent:
				if aborted {
					t.Fatal("received TurnCompleted, want TurnAborted")
				}
				return
			case protocol.TurnAbortedEvent:
				if !aborted {
					t.Fatal("received TurnAborted, want TurnCompleted")
				}
				return
			case protocol.StreamErrorEvent:
				t.Fatalf("stream error: %#v", event.Msg)
			}
		case <-timeout:
			t.Fatal("timed out waiting for terminal event")
		}
	}
}
