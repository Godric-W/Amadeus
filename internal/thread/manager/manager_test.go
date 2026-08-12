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
	"github.com/Godric-W/Amadeus/internal/agent/task"
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

func (factory testFactory) RegularTask(string) (task.SessionTask, error) {
	return task.FuncTask{TaskKind: task.KindRegular, RunFunc: func(ctx context.Context, _ task.Host, _ *turn.Context, _ []task.Input) (task.Result, error) {
		if factory.block {
			<-ctx.Done()
			return task.Result{}, ctx.Err()
		}
		item, err := rollout.NewRawItem(rollout.KindResponseItem, json.RawMessage(`{"type":"assistant_message","role":"assistant","content":"done"}`))
		return task.Result{Items: []rollout.Item{item}}, err
	}}, nil
}

func (factory testFactory) CompactTask() (task.SessionTask, error) {
	return factory.RegularTask("")
}

func (factory concurrentHistoryFactory) RegularTask(string) (task.SessionTask, error) {
	return task.FuncTask{TaskKind: task.KindRegular, RunFunc: func(ctx context.Context, host task.Host, turnContext *turn.Context, _ []task.Input) (task.Result, error) {
		var wait sync.WaitGroup
		errorsChannel := make(chan error, 16)
		for index := range 16 {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				item, err := rollout.NewRawItem(rollout.KindResponseItem, json.RawMessage(`{"type":"assistant_message","role":"assistant","content":"parallel"}`))
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
				return task.Result{}, err
			}
		}
		factory.observed <- len(host.History())
		return task.Result{}, nil
	}}, nil
}

func (factory concurrentHistoryFactory) CompactTask() (task.SessionTask, error) {
	return factory.RegularTask("")
}

func (store terminalFailStore) AppendItems(ctx context.Context, id thread.ID, turnID thread.TurnID, items ...rollout.Item) (thread.AppendResult, error) {
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
		switch event.Message.(type) {
		case protocol.StreamError:
			streamError = true
		case protocol.TurnCompleted, protocol.TurnAborted:
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
				if rejected, ok := event.Message.(protocol.TurnRejected); ok {
					if rejected.Error != "materialize failed" {
						t.Fatalf("rejected = %#v", rejected)
					}
					goto nextAttempt
				}
			case <-timeout:
				t.Fatal("timed out waiting for TurnRejected")
			}
		}
	nextAttempt:
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
	userItem, _ := rollout.NewRawItem(rollout.KindResponseItem, json.RawMessage(`{"type":"user_message","role":"user","content":"run"}`))
	startedItem, _ := rollout.NewItem(rollout.KindTurnStarted, rollout.TurnStarted{Input: "run"})
	toolCall, _ := rollout.NewRawItem(rollout.KindResponseItem, json.RawMessage(`{"type":"tool_call","call_id":"call-1","name":"execute_command"}`))
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

func newTestManager(t *testing.T, ctx context.Context, factory task.Factory) (*ThreadManager, thread.ThreadStore) {
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
		DefaultTaskFactory: factory,
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
		PermissionMode: turn.PermissionModeDefault,
	}
}

func testTurnContext(t *testing.T, threadID rollout.ThreadID, turnID rollout.TurnID) turn.Context {
	t.Helper()
	configuration := testConfiguration(t)
	return turn.Context{
		ThreadID: threadID, TurnID: turnID, Provider: configuration.Provider, Model: configuration.Model,
		CWD: configuration.CWD, InitialPermissionMode: turn.PermissionModeDefault,
	}
}

func waitForStarted(t *testing.T, io session.SessionIo) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-io.Events:
			if _, ok := event.Message.(protocol.TurnStarted); ok {
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
			switch event.Message.(type) {
			case protocol.TurnCompleted:
				if aborted {
					t.Fatal("received TurnCompleted, want TurnAborted")
				}
				return
			case protocol.TurnAborted:
				if !aborted {
					t.Fatal("received TurnAborted, want TurnCompleted")
				}
				return
			case protocol.StreamError:
				t.Fatalf("stream error: %#v", event.Message)
			}
		case <-timeout:
			t.Fatal("timed out waiting for terminal event")
		}
	}
}
