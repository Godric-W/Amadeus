package goal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/threadstore"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type fakeAutomation struct {
	mu        sync.Mutex
	starts    []agentsession.ResponseItemTurnInput
	injected  []agentsession.ResponseItemTurnInput
	events    []protocol.Event
	persisted []protocol.ThreadGoalUpdatedEvent
}

func (automation *fakeAutomation) Emit(event protocol.Event) {
	automation.mu.Lock()
	automation.events = append(automation.events, event)
	automation.mu.Unlock()
}

func (automation *fakeAutomation) StartTurnIfIdle(_ context.Context, input agentsession.TurnInput) (agentsession.StartIfIdleSubmission, error) {
	response, ok := input.(agentsession.ResponseItemTurnInput)
	if !ok {
		return agentsession.StartIfIdleSubmission{}, errors.New("unexpected automatic input")
	}
	automation.mu.Lock()
	automation.starts = append(automation.starts, response)
	automation.mu.Unlock()
	return agentsession.StartIfIdleSubmission{TurnID: "goal-turn"}, nil
}

func (automation *fakeAutomation) InjectIfRunning(_ context.Context, input agentsession.ResponseItemTurnInput) error {
	automation.mu.Lock()
	automation.injected = append(automation.injected, input)
	automation.mu.Unlock()
	return nil
}

func (automation *fakeAutomation) PersistGoalUpdate(_ context.Context, event protocol.ThreadGoalUpdatedEvent) error {
	automation.mu.Lock()
	automation.persisted = append(automation.persisted, event)
	automation.mu.Unlock()
	return nil
}

type goalHarness struct {
	extension  *Extension
	service    *Service
	runtime    *statesqlite.Runtime
	threadData *extension.Data
	turnData   *extension.Data
	automation *fakeAutomation
	threadID   protocol.ThreadID
}

func newGoalHarness(t *testing.T, clock func() time.Time) *goalHarness {
	t.Helper()
	ctx := context.Background()
	runtime, err := statesqlite.Open(ctx, t.TempDir(), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	service, err := NewService(runtime)
	if err != nil {
		t.Fatal(err)
	}
	automation := &fakeAutomation{}
	builder := extension.NewBuilder(automation)
	goalExtension, err := Install(builder, runtime, service, clock)
	if err != nil {
		t.Fatal(err)
	}
	threadID := testutil.ThreadID(1)
	sessionData, _ := extension.NewData(testutil.SessionID(1).String())
	threadData, _ := extension.NewData(threadID.String())
	turnData, _ := extension.NewData("turn-1")
	extension.Set[agentsession.ThreadAutomation](threadData, agentsession.ThreadAutomation(automation))
	configured := config.Default()
	if err := goalExtension.OnThreadStart(ctx, extension.ThreadStartInput{Config: configured, SessionSource: protocol.RootSessionSource(), PersistentThreadStateAvailable: true, SessionData: sessionData, ThreadData: threadData}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = goalExtension.OnThreadStop(context.Background(), extension.ThreadStopInput{SessionData: sessionData, ThreadData: threadData})
	})
	return &goalHarness{extension: goalExtension, service: service, runtime: runtime, threadData: threadData, turnData: turnData, automation: automation, threadID: threadID}
}

func (harness *goalHarness) startTurn(t *testing.T, usage llm.TokenUsage) {
	t.Helper()
	if err := harness.extension.OnTurnStart(context.Background(), extension.TurnStartInput{TurnID: "turn-1", Mode: protocol.ModeKindDefault, TokenUsageAtStart: usage, ThreadData: harness.threadData, TurnData: harness.turnData}); err != nil {
		t.Fatal(err)
	}
}

func TestExternalSetPublishesBeforeAutomaticContinuation(t *testing.T) {
	harness := newGoalHarness(t, time.Now)
	status := protocol.ThreadGoalActive
	outcome, err := harness.service.Set(context.Background(), SetRequest{ThreadID: harness.threadID, Objective: ObjectiveUpdate{Set: true, Value: "ship <goal>"}, Status: &status})
	if err != nil {
		t.Fatal(err)
	}
	if err := outcome.PublishAndApply(context.Background()); err != nil {
		t.Fatal(err)
	}
	harness.automation.mu.Lock()
	defer harness.automation.mu.Unlock()
	if len(harness.automation.persisted) != 1 || len(harness.automation.events) != 1 || len(harness.automation.starts) != 1 {
		t.Fatalf("persisted/events/starts = %d/%d/%d", len(harness.automation.persisted), len(harness.automation.events), len(harness.automation.starts))
	}
	content := harness.automation.starts[0].Item.Content
	if !strings.Contains(content, "ship &lt;goal&gt;") || !strings.Contains(content, "Tokens remaining: unbounded") {
		t.Fatalf("continuation input = %q", content)
	}
}

func TestParallelToolFinishAccountsLatestUsageOnceAndInjectsBudget(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	harness := newGoalHarness(t, func() time.Time { return now })
	harness.startTurn(t, llm.TokenUsage{})
	runtime := runtimeFromData(harness.threadData)
	budget := int64(20)
	created, err := runtime.store.InsertIfAbsentOrComplete(context.Background(), harness.threadID, "budgeted goal", protocol.ThreadGoalActive, &budget)
	if err != nil {
		t.Fatal(err)
	}
	runtime.accounting.markCurrentGoalActive(created.GoalID)
	if err := harness.extension.OnTokenUsage(context.Background(), extension.TokenUsageInput{ThreadData: harness.threadData, Usage: protocol.TokenUsageInfo{TotalTokenUsage: llm.TokenUsage{InputTokens: 20, CachedInputTokens: 5, OutputTokens: 10, TotalTokens: 30}}}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for _, callID := range []string{"call-1", "call-2"} {
		wait.Add(1)
		go func(callID string) {
			defer wait.Done()
			_ = harness.extension.OnToolFinish(context.Background(), extension.ToolFinishInput{TurnID: "turn-1", CallID: callID, ToolName: "read", Outcome: extension.ToolCallCompleted, ThreadData: harness.threadData})
		}(callID)
	}
	wait.Wait()
	goal, err := runtime.store.Get(context.Background(), harness.threadID)
	if err != nil {
		t.Fatal(err)
	}
	if goal.TokensUsed != 25 || goal.Status != protocol.ThreadGoalBudgetLimited {
		t.Fatalf("goal = %#v", goal)
	}
	harness.automation.mu.Lock()
	injected := len(harness.automation.injected)
	harness.automation.mu.Unlock()
	if injected != 1 {
		t.Fatalf("budget injections = %d", injected)
	}
}

func TestTurnErrorStopsGoalBeforeIdleContinuation(t *testing.T) {
	harness := newGoalHarness(t, time.Now)
	harness.startTurn(t, llm.TokenUsage{})
	runtime := runtimeFromData(harness.threadData)
	goal, err := runtime.store.InsertIfAbsentOrComplete(context.Background(), harness.threadID, "error goal", protocol.ThreadGoalActive, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.accounting.markCurrentGoalActive(goal.GoalID)
	if err := harness.extension.OnTurnError(context.Background(), extension.TurnErrorInput{TurnID: "turn-1", Err: errors.New("terminal provider failure"), ThreadData: harness.threadData}); err != nil {
		t.Fatal(err)
	}
	stopped, err := runtime.store.Get(context.Background(), harness.threadID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != protocol.ThreadGoalBlocked {
		t.Fatalf("status = %s", stopped.Status)
	}
	if err := harness.extension.OnThreadIdle(context.Background(), extension.ThreadIdleInput{ThreadData: harness.threadData}); err != nil {
		t.Fatal(err)
	}
	if len(harness.automation.starts) != 0 {
		t.Fatal("blocked goal continued")
	}
}

func TestGoalToolsCreateAndComplete(t *testing.T) {
	harness := newGoalHarness(t, time.Now)
	harness.startTurn(t, llm.TokenUsage{})
	runtime := runtimeFromData(harness.threadData)
	create := newGoalTool(goalToolCreate, runtime, nil)
	createResult := executeGoalTool(t, create, "call-create", `{"objective":"finish tool lifecycle"}`)
	if !strings.Contains(createResult.Text, `"status":"active"`) {
		t.Fatalf("create result = %s", createResult.Text)
	}
	update := newGoalTool(goalToolUpdate, runtime, nil)
	updateResult := executeGoalTool(t, update, "call-update", `{"status":"complete"}`)
	if !strings.Contains(updateResult.Text, `"status":"complete"`) {
		t.Fatalf("update result = %s", updateResult.Text)
	}
	stored, err := runtime.store.Get(context.Background(), harness.threadID)
	if err != nil || stored.Status != protocol.ThreadGoalComplete {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestGoalServiceRejectsDirectSubAgentMutation(t *testing.T) {
	ctx := context.Background()
	runtime, err := statesqlite.Open(ctx, t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	service, err := NewService(runtime)
	if err != nil {
		t.Fatal(err)
	}
	parentID, childID := testutil.ThreadID(10), testutil.ThreadID(11)
	now := time.Now().UTC()
	if err := runtime.Threads().UpsertThread(ctx, threadstore.StoredThread{
		ID: childID, Source: protocol.NewSubAgentSessionSource(parentID, 1, "atlas", "explorer"),
		AgentEdgeState: protocol.AgentSpawnEdgeOpen, RolloutPath: "/workspace/rollout-" + childID.String() + ".jsonl", CWD: "/workspace", Title: "Child",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	status := protocol.ThreadGoalActive
	_, err = service.Set(ctx, SetRequest{ThreadID: childID, Objective: ObjectiveUpdate{Set: true, Value: "mutate child"}, Status: &status})
	if err == nil || err.Error() != "sub-agent threads do not support direct goal mutation" {
		t.Fatalf("sub-agent set error = %v", err)
	}
}

func executeGoalTool(t *testing.T, candidate tool.ToolDefinition, callID, arguments string) tool.ToolResult {
	t.Helper()
	invocation := tool.Invocation{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Call: tool.NewCall(callID, candidate.Spec().Name, json.RawMessage(arguments)), Source: tool.ToolCallSourceModel}
	toolContext := tool.ToolUseContext{Context: context.Background(), Invocation: invocation}
	if err := candidate.ValidateInput(toolContext, invocation); err != nil {
		t.Fatal(err)
	}
	prepared, err := candidate.Prepare(toolContext, invocation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := candidate.Execute(toolContext, prepared)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

var _ agentsession.ThreadAutomation = (*fakeAutomation)(nil)
