package multiagent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type testHost struct {
	mu            sync.Mutex
	next          int
	fail          bool
	runtimes      map[protocol.ThreadID]*testRuntime
	notifications []Notification
}

func (host *testHost) SpawnChild(_ context.Context, _ *Control, _ SpawnChildRequest) (AgentRuntime, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.fail {
		return nil, errors.New("spawn failed")
	}
	host.next++
	id := protocol.ThreadID("child-" + string(rune('0'+host.next)))
	runtime := &testRuntime{id: id, events: make(chan protocol.Event, 8), terminated: make(chan struct{})}
	if host.runtimes == nil {
		host.runtimes = make(map[protocol.ThreadID]*testRuntime)
	}
	host.runtimes[id] = runtime
	return runtime, nil
}
func (host *testHost) NotifyParent(_ context.Context, _ protocol.ThreadID, notification Notification) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.notifications = append(host.notifications, notification)
	return nil
}

type testRuntime struct {
	id          protocol.ThreadID
	events      chan protocol.Event
	terminated  chan struct{}
	closeOnce   sync.Once
	mu          sync.Mutex
	shutdownErr error
	submitHook  func(protocol.Op) error
	submissions []protocol.Op
	userInputs  []protocol.UserInputOp
}

func (runtime *testRuntime) ID() protocol.ThreadID { return runtime.id }
func (runtime *testRuntime) Submit(_ context.Context, op protocol.Op) error {
	runtime.mu.Lock()
	runtime.submissions = append(runtime.submissions, op)
	hook := runtime.submitHook
	runtime.mu.Unlock()
	if hook != nil {
		return hook(op)
	}
	return nil
}
func (runtime *testRuntime) SubmitUserInput(_ context.Context, op protocol.UserInputOp) error {
	runtime.mu.Lock()
	runtime.userInputs = append(runtime.userInputs, op)
	runtime.mu.Unlock()
	return nil
}
func (runtime *testRuntime) Shutdown(context.Context) error {
	runtime.mu.Lock()
	shutdownErr := runtime.shutdownErr
	runtime.mu.Unlock()
	if shutdownErr != nil {
		return shutdownErr
	}
	runtime.closeOnce.Do(func() { close(runtime.terminated); close(runtime.events) })
	return nil
}
func (runtime *testRuntime) Events() <-chan protocol.Event { return runtime.events }
func (runtime *testRuntime) Terminated() <-chan struct{}   { return runtime.terminated }

func TestControlSpawnStatusWaitAndClose(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 2, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), "root", "inspect architecture")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{ThreadID: spawned.AgentID}}
	runtime.events <- protocol.Event{Msg: protocol.ItemCompletedEvent{Item: protocol.TurnItem{Kind: protocol.ItemAssistantMessage, Text: "evidence"}}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waited, err := control.Wait(ctx, []protocol.ThreadID{spawned.AgentID}, time.Second)
	statuses := waited.Statuses
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status.Kind != protocol.AgentStatusCompleted || statuses[0].Status.Message != "evidence" {
		t.Fatalf("statuses = %#v", statuses)
	}
	previous, err := control.CloseAgent(ctx, spawned.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Kind != protocol.AgentStatusCompleted || control.Snapshot(spawned.AgentID).Status.Kind != protocol.AgentStatusNotFound {
		t.Fatalf("previous=%#v snapshot=%#v", previous, control.Snapshot(spawned.AgentID))
	}
}

func TestControlRollsBackFailedReservation(t *testing.T) {
	host := &testHost{fail: true}
	control, err := NewControl("root", host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Spawn(context.Background(), "root", "first"); err == nil {
		t.Fatal("expected spawn failure")
	}
	host.fail = false
	spawned, err := control.Spawn(context.Background(), "root", "second")
	if err != nil {
		t.Fatal(err)
	}
	if spawned.Nickname != "atlas" {
		t.Fatalf("nickname = %q", spawned.Nickname)
	}
	_ = control.Close(context.Background())
}

func TestStatusFromEventMatchesCodexLifecycle(t *testing.T) {
	tests := []struct {
		message protocol.EventMsg
		kind    protocol.AgentStatusKind
	}{
		{protocol.TurnStartedEvent{}, protocol.AgentStatusRunning},
		{protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted}, protocol.AgentStatusCompleted},
		{protocol.TurnCompleteEvent{Status: protocol.TurnStatusFailed, Error: "failed"}, protocol.AgentStatusErrored},
		{protocol.TurnAbortedEvent{}, protocol.AgentStatusInterrupted},
		{protocol.ShutdownCompleteEvent{}, protocol.AgentStatusShutdown},
	}
	for _, test := range tests {
		status, ok := statusFromEvent(test.message, "done")
		if !ok || status.Kind != test.kind {
			t.Fatalf("%T => %#v, %v", test.message, status, ok)
		}
	}
}

func TestControlEnforcesConcurrentSlotsAndUniqueNicknames(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 2, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result SpawnResult
		err    error
	}
	outcomes := make(chan outcome, 8)
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, spawnErr := control.Spawn(context.Background(), "root", "inspect one independent area")
			outcomes <- outcome{result: result, err: spawnErr}
		}()
	}
	workers.Wait()
	close(outcomes)
	successes := make([]SpawnResult, 0, 2)
	for outcome := range outcomes {
		if outcome.err == nil {
			successes = append(successes, outcome.result)
		}
	}
	if len(successes) != 2 {
		t.Fatalf("successful spawns = %#v", successes)
	}
	if successes[0].Nickname == successes[1].Nickname {
		t.Fatalf("duplicate nickname %q", successes[0].Nickname)
	}
	if _, err := control.Spawn(context.Background(), successes[0].AgentID, "nested task"); err == nil {
		t.Fatal("nested spawn exceeded max depth")
	}
	if err := control.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestControlWaitTimeoutReturnsPendingSnapshot(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), "root", "inspect")
	if err != nil {
		t.Fatal(err)
	}
	waited, err := control.Wait(context.Background(), []protocol.ThreadID{spawned.AgentID}, 5*time.Millisecond)
	statuses := waited.Statuses
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status.Kind != protocol.AgentStatusPendingInit {
		t.Fatalf("timeout statuses = %#v", statuses)
	}
	if !waited.TimedOut {
		t.Fatal("wait timeout did not set timed_out")
	}
	_ = control.Close(context.Background())
}

func TestControlDeduplicatesErroredTurnNotification(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), "root", "inspect")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{}}
	runtime.events <- protocol.Event{Msg: protocol.ErrorEvent{Message: "failed"}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{Status: protocol.TurnStatusFailed, Error: "failed with detail"}}
	deadline := time.Now().Add(time.Second)
	for {
		host.mu.Lock()
		count := len(host.notifications)
		host.mu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("notification count = %d", count)
		}
		time.Sleep(time.Millisecond)
	}
	_ = control.Close(context.Background())
}

func TestControlRetainsSlotUntilFailedShutdownIsRetried(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), "root", "inspect")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.mu.Lock()
	runtime.shutdownErr = errors.New("shutdown failed")
	runtime.mu.Unlock()
	if _, err := control.CloseAgent(context.Background(), spawned.AgentID); err == nil {
		t.Fatal("expected shutdown failure")
	}
	if snapshot := control.Snapshot(spawned.AgentID); snapshot.Status.Kind == protocol.AgentStatusNotFound {
		t.Fatal("failed shutdown released the agent record")
	}
	if _, err := control.Spawn(context.Background(), "root", "second"); err == nil {
		t.Fatal("failed shutdown released the agent slot")
	}
	runtime.mu.Lock()
	runtime.shutdownErr = nil
	runtime.mu.Unlock()
	if _, err := control.CloseAgent(context.Background(), spawned.AgentID); err != nil {
		t.Fatal(err)
	}
	respawned, err := control.Spawn(context.Background(), "root", "second")
	if err != nil {
		t.Fatal(err)
	}
	if respawned.Nickname != spawned.Nickname {
		t.Fatalf("nickname = %q, want released %q", respawned.Nickname, spawned.Nickname)
	}
	_ = control.Close(context.Background())
}

func TestControlSendInputInterruptsRunningAgentBeforeNewTurn(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), "root", "initial")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.mu.Lock()
	runtime.submitHook = func(op protocol.Op) error {
		if _, ok := op.(protocol.InterruptOp); ok {
			runtime.events <- protocol.Event{Msg: protocol.TurnAbortedEvent{}}
		}
		return nil
	}
	runtime.mu.Unlock()
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{}}
	waitForAgentStatus(t, control, spawned.AgentID, protocol.AgentStatusRunning)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := control.SendInput(ctx, spawned.AgentID, "follow up", true); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.submissions) != 1 {
		t.Fatalf("interrupt submissions = %#v", runtime.submissions)
	}
	if _, ok := runtime.submissions[0].(protocol.InterruptOp); !ok {
		t.Fatalf("submission = %T, want InterruptOp", runtime.submissions[0])
	}
	if len(runtime.userInputs) != 2 || runtime.userInputs[1].Content != "follow up" {
		t.Fatalf("user inputs = %#v", runtime.userInputs)
	}
}

func TestControlSendInputStartsNewTurnAfterCompletion(t *testing.T) {
	host := &testHost{}
	control, err := NewControl("root", host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), "root", "initial")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted}}
	waitForAgentStatus(t, control, spawned.AgentID, protocol.AgentStatusCompleted)
	if err := control.SendInput(context.Background(), spawned.AgentID, "next turn", false); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.submissions) != 0 {
		t.Fatalf("unexpected control submissions = %#v", runtime.submissions)
	}
	if len(runtime.userInputs) != 2 || runtime.userInputs[1].Content != "next turn" {
		t.Fatalf("user inputs = %#v", runtime.userInputs)
	}
}

func waitForAgentStatus(t *testing.T, control *Control, id protocol.ThreadID, want protocol.AgentStatusKind) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if status := control.Snapshot(id).Status.Kind; status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent status = %q, want %q", control.Snapshot(id).Status.Kind, want)
		}
		time.Sleep(time.Millisecond)
	}
}
