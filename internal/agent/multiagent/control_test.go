package multiagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type testHost struct {
	mu             sync.Mutex
	next           int
	fail           bool
	edgeFailures   int
	notifyFailures int
	runtimes       map[protocol.ThreadID]*testRuntime
	notifications  []Notification
	edges          []testEdge
}

type testEdge struct {
	parent protocol.ThreadID
	agent  protocol.ThreadID
	state  protocol.AgentSpawnEdgeState
}

func (host *testHost) SpawnChild(_ context.Context, _ *Control, _ SpawnChildRequest) (AgentRuntime, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.fail {
		return nil, errors.New("spawn failed")
	}
	host.next++
	id := testutil.ThreadID(uint64(100 + host.next))
	runtime := &testRuntime{id: id, events: make(chan protocol.Event, 8), terminated: make(chan struct{})}
	if host.runtimes == nil {
		host.runtimes = make(map[protocol.ThreadID]*testRuntime)
	}
	host.runtimes[id] = runtime
	return runtime, nil
}
func (host *testHost) ResumeChild(_ context.Context, _ *Control, id protocol.ThreadID) (AgentRuntime, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	runtime := host.runtimes[id]
	if runtime == nil {
		return nil, errors.New("resume failed")
	}
	return runtime, nil
}

func (host *testHost) RecordSpawnEdge(_ context.Context, parent, agent protocol.ThreadID, state protocol.AgentSpawnEdgeState) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.edgeFailures > 0 {
		host.edgeFailures--
		return errors.New("agent edge persistence failed")
	}
	host.edges = append(host.edges, testEdge{parent: parent, agent: agent, state: state})
	return nil
}
func (host *testHost) NotifyParent(_ context.Context, _ protocol.ThreadID, notification Notification) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.notifyFailures > 0 {
		host.notifyFailures--
		return errors.New("notification persistence failed")
	}
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
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 2, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect architecture")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	finalMessage := "evidence"
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{ThreadID: spawned.AgentID, TurnID: "turn-1"}}
	runtime.events <- protocol.Event{Msg: protocol.ItemCompletedEvent{Item: protocol.TurnItem{Kind: protocol.ItemAssistantMessage, Text: "not the final answer"}}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, LastAgentMessage: &finalMessage}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waited, err := control.Wait(ctx, []protocol.ThreadID{spawned.AgentID}, time.Second)
	statuses := waited.Statuses
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status.Kind != protocol.AgentStatusCompleted || statuses[0].Status.Message != "evidence" || statuses[0].LastTurn == nil || statuses[0].LastTurn.Outcome != protocol.TurnOutcomeCompleted {
		t.Fatalf("statuses = %#v", statuses)
	}
	previous, err := control.CloseAgent(ctx, spawned.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Kind != protocol.AgentStatusCompleted || control.Snapshot(spawned.AgentID).Status.Kind != protocol.AgentStatusNotFound {
		t.Fatalf("previous=%#v snapshot=%#v", previous, control.Snapshot(spawned.AgentID))
	}
	host.mu.Lock()
	edges := append([]testEdge(nil), host.edges...)
	notificationCount := len(host.notifications)
	host.mu.Unlock()
	if len(edges) != 2 || edges[0].state != protocol.AgentSpawnEdgeOpen || edges[1].state != protocol.AgentSpawnEdgeClosed {
		t.Fatalf("spawn edge lifecycle = %#v", edges)
	}
	if notificationCount != 1 {
		t.Fatalf("completion plus close notifications = %d", notificationCount)
	}
}

func TestControlRollsBackFailedReservation(t *testing.T) {
	host := &testHost{fail: true}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Spawn(context.Background(), testutil.ThreadID(1), "first"); err == nil {
		t.Fatal("expected spawn failure")
	}
	host.fail = false
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "second")
	if err != nil {
		t.Fatal(err)
	}
	if spawned.Nickname != "atlas" {
		t.Fatalf("nickname = %q", spawned.Nickname)
	}
	_ = control.Close(context.Background())
}

func TestControlRollsBackRuntimeWhenOpenEdgePersistenceFails(t *testing.T) {
	host := &testHost{edgeFailures: 1}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Spawn(context.Background(), testutil.ThreadID(1), "first"); err == nil || !strings.Contains(err.Error(), "persist open agent edge") {
		t.Fatalf("spawn edge failure = %v", err)
	}
	if records := control.SnapshotAll(); len(records) != 0 {
		t.Fatalf("failed spawn retained records = %#v", records)
	}
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "second")
	if err != nil {
		t.Fatal(err)
	}
	if spawned.Nickname != "atlas" {
		t.Fatalf("nickname after edge rollback = %q", spawned.Nickname)
	}
	_ = control.Close(context.Background())
}

func TestReduceLifecycleEventMatchesCodexLifecycle(t *testing.T) {
	tests := []struct {
		message protocol.EventMsg
		kind    protocol.AgentStatusKind
	}{
		{protocol.TurnStartedEvent{TurnID: "turn-1"}, protocol.AgentStatusRunning},
		{protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted}, protocol.AgentStatusCompleted},
		{protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusFailed, Outcome: protocol.TurnOutcomeFailed, Error: "failed"}, protocol.AgentStatusErrored},
		{protocol.TurnAbortedEvent{TurnID: "turn-1"}, protocol.AgentStatusInterrupted},
		{protocol.ShutdownCompleteEvent{}, protocol.AgentStatusShutdown},
	}
	for _, test := range tests {
		state, changed, _ := ReduceLifecycleEvent(InitialLifecycleState(), test.message)
		if !changed || state.Status.Kind != test.kind {
			t.Fatalf("%T => %#v, %v", test.message, state, changed)
		}
	}
}

func TestControlEnforcesConcurrentSlotsAndUniqueNicknames(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 2, MaxDepth: 1})
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
			result, spawnErr := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect one independent area")
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
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	waited, err := control.Wait(context.Background(), []protocol.ThreadID{spawned.AgentID}, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(waited.Statuses) != 0 {
		t.Fatalf("timeout final statuses = %#v", waited.Statuses)
	}
	if !waited.TimedOut {
		t.Fatal("wait timeout did not set timed_out")
	}
	_ = control.Close(context.Background())
}

func TestControlWaitReturnsWhenAnyAgentIsFinal(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 2, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	first, err := control.Spawn(context.Background(), testutil.ThreadID(1), "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := control.Spawn(context.Background(), testutil.ThreadID(1), "second")
	if err != nil {
		t.Fatal(err)
	}
	host.runtimes[first.AgentID].events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-first"}}
	host.runtimes[second.AgentID].events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-second"}}
	waitForAgentStatus(t, control, first.AgentID, protocol.AgentStatusRunning)
	waitForAgentStatus(t, control, second.AgentID, protocol.AgentStatusRunning)

	result := make(chan WaitResult, 1)
	go func() {
		waited, _ := control.Wait(context.Background(), []protocol.ThreadID{first.AgentID, second.AgentID}, time.Second)
		result <- waited
	}()
	final := "first result"
	host.runtimes[first.AgentID].events <- protocol.Event{Msg: protocol.TurnCompleteEvent{
		TurnID: "turn-first", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, LastAgentMessage: &final,
	}}
	select {
	case waited := <-result:
		if waited.TimedOut || len(waited.Statuses) != 1 || waited.Statuses[0].AgentID != first.AgentID {
			t.Fatalf("wait-any result = %#v", waited)
		}
	case <-time.After(time.Second):
		t.Fatal("wait_agent waited for all agents")
	}
	if status := control.Snapshot(second.AgentID).Status.Kind; status != protocol.AgentStatusRunning {
		t.Fatalf("second agent status = %q", status)
	}
}

func TestBlockedTurnPreservesReasonAndIgnoresAssistantPreamble(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	runtime.events <- protocol.Event{Msg: protocol.ItemCompletedEvent{Item: protocol.TurnItem{Kind: protocol.ItemAssistantMessage, Text: "I will inspect files."}}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{
		TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeBlocked,
		Reason: "internal model sample safety limit reached: 20",
	}}
	waited, err := control.Wait(context.Background(), []protocol.ThreadID{spawned.AgentID}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(waited.Statuses) != 1 || waited.Statuses[0].Status.Message != "" || waited.Statuses[0].LastTurn == nil || waited.Statuses[0].LastTurn.Outcome != protocol.TurnOutcomeBlocked || !strings.Contains(waited.Statuses[0].LastTurn.Reason, "20") {
		t.Fatalf("blocked snapshot = %#v", waited.Statuses)
	}
}

func TestNotificationFailureRemainsPendingUntilWaitRetry(t *testing.T) {
	host := &testHost{notifyFailures: 1}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	message := "done"
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, LastAgentMessage: &message}}
	waitForAgentStatus(t, control, spawned.AgentID, protocol.AgentStatusCompleted)
	deadline := time.Now().Add(time.Second)
	for control.Snapshot(spawned.AgentID).NotificationError == "" {
		if time.Now().After(deadline) {
			t.Fatal("notification failure was not retained")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := control.Wait(context.Background(), []protocol.ThreadID{spawned.AgentID}, time.Second); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	notificationCount := len(host.notifications)
	host.mu.Unlock()
	if notificationCount != 1 || control.Snapshot(spawned.AgentID).NotificationError != "" {
		t.Fatalf("notification retry count=%d snapshot=%#v", notificationCount, control.Snapshot(spawned.AgentID))
	}
}

func TestControlDeduplicatesErroredTurnNotification(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	runtime.events <- protocol.Event{Msg: protocol.ErrorEvent{Message: "failed"}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusFailed, Outcome: protocol.TurnOutcomeFailed, Error: "failed with detail"}}
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

func TestWaitDoesNotTreatIntermediateErrorAsTerminalResult(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	runtime.events <- protocol.Event{Msg: protocol.ErrorEvent{TurnID: "turn-1", Message: "provider failed"}}
	waitForAgentStatus(t, control, spawned.AgentID, protocol.AgentStatusErrored)
	waited, err := control.Wait(context.Background(), []protocol.ThreadID{spawned.AgentID}, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !waited.TimedOut || len(waited.Statuses) != 0 {
		t.Fatalf("intermediate error wait = %#v", waited)
	}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusFailed, Outcome: protocol.TurnOutcomeFailed, Error: "provider failed"}}
	waited, err = control.Wait(context.Background(), []protocol.ThreadID{spawned.AgentID}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(waited.Statuses) != 1 || waited.Statuses[0].LastTurn == nil || waited.Statuses[0].LastTurn.Outcome != protocol.TurnOutcomeFailed {
		t.Fatalf("terminal error wait = %#v", waited)
	}
}

func TestControlRetainsSlotUntilFailedShutdownIsRetried(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
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
	if _, err := control.Spawn(context.Background(), testutil.ThreadID(1), "second"); err == nil {
		t.Fatal("failed shutdown released the agent slot")
	}
	runtime.mu.Lock()
	runtime.shutdownErr = nil
	runtime.mu.Unlock()
	if _, err := control.CloseAgent(context.Background(), spawned.AgentID); err != nil {
		t.Fatal(err)
	}
	respawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "second")
	if err != nil {
		t.Fatal(err)
	}
	if respawned.Nickname != spawned.Nickname {
		t.Fatalf("nickname = %q, want released %q", respawned.Nickname, spawned.Nickname)
	}
	_ = control.Close(context.Background())
}

func TestControlDoesNotReportCloseWhenClosedEdgePersistenceFails(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	host.edgeFailures = 1
	host.mu.Unlock()
	if _, err := control.CloseAgent(context.Background(), spawned.AgentID); err == nil || !strings.Contains(err.Error(), "persist closed agent edge") {
		t.Fatalf("close edge failure = %v", err)
	}
	if snapshot := control.Snapshot(spawned.AgentID); snapshot.Status.Kind == protocol.AgentStatusNotFound {
		t.Fatal("failed close edge persistence removed the live agent")
	}
	select {
	case <-host.runtimes[spawned.AgentID].Terminated():
		t.Fatal("failed close edge persistence shut down the runtime")
	default:
	}
	if _, err := control.CloseAgent(context.Background(), spawned.AgentID); err != nil {
		t.Fatal(err)
	}
}

func TestControlSendInputInterruptsRunningAgentBeforeNewTurn(t *testing.T) {
	host := &testHost{}
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "initial")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.mu.Lock()
	runtime.submitHook = func(op protocol.Op) error {
		if _, ok := op.(protocol.InterruptOp); ok {
			runtime.events <- protocol.Event{Msg: protocol.TurnAbortedEvent{TurnID: "turn-1", Reason: "interrupted"}}
		}
		return nil
	}
	runtime.mu.Unlock()
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
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
	control, err := NewControl(testutil.SessionID(1), testutil.ThreadID(1), host, Options{MaxAgents: 1, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	spawned, err := control.Spawn(context.Background(), testutil.ThreadID(1), "initial")
	if err != nil {
		t.Fatal(err)
	}
	runtime := host.runtimes[spawned.AgentID]
	runtime.events <- protocol.Event{Msg: protocol.TurnStartedEvent{TurnID: "turn-1"}}
	runtime.events <- protocol.Event{Msg: protocol.TurnCompleteEvent{TurnID: "turn-1", Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted}}
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
