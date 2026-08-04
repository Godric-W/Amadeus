package plan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type planClient struct {
	responses []llm.Response
	index     int
}

func (client *planClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	if client.index >= len(client.responses) {
		return llm.Response{}, errors.New("unexpected Plan LLM call")
	}
	response := client.responses[client.index]
	client.index++
	return response, nil
}

func (*planClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("stream is not supported")
}

func (*planClient) Model() llm.ModelInfo { return llm.ModelInfo{Provider: "test", Name: "test-model"} }

func (*planClient) Capabilities() llm.Capabilities { return llm.Capabilities{} }

func TestParsePlanDraftAcceptsCommonFormats(t *testing.T) {
	for name, content := range map[string]string{
		"bullets":  "PLAN\n- inspect files\n- report result",
		"numbers":  "1. inspect files\n2) report result",
		"fallback": "inspect the requested directory",
	} {
		t.Run(name, func(t *testing.T) {
			draft, err := ParsePlanDraft(content)
			if err != nil {
				t.Fatalf("parse plan: %v", err)
			}
			if len(draft.Tasks) == 0 {
				t.Fatal("plan contains no tasks")
			}
		})
	}
}

func TestBuildSerialGraphCreatesProgramOwnedDAG(t *testing.T) {
	graph, err := BuildSerialGraph(PlanDraft{Tasks: []string{"inspect", "report"}}, 2)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if graph.Version != 2 || len(graph.Tasks) != 2 || graph.Tasks[0].ID != "task-1" || graph.Tasks[1].ID != "task-2" {
		t.Fatalf("unexpected graph: %#v", graph)
	}
	if len(graph.Tasks[1].Dependencies) != 1 || graph.Tasks[1].Dependencies[0] != graph.Tasks[0].ID {
		t.Fatalf("graph is not serial: %#v", graph.Tasks)
	}
}

func TestNormalizePlanForConversationalGreeting(t *testing.T) {
	draft := PlanDraft{Tasks: []string{"inspect files", "run tests", "summarize", "report"}, Usage: llm.Usage{TotalTokens: 42}}
	normalized := normalizePlanForObjective("你好！", draft)
	if len(normalized.Tasks) != 1 {
		t.Fatalf("greeting plan was not collapsed: %#v", normalized.Tasks)
	}
	if !strings.Contains(normalized.Tasks[0], "你好") || !strings.Contains(normalized.Tasks[0], "without calling tools") {
		t.Fatalf("unexpected greeting task: %q", normalized.Tasks[0])
	}
	if normalized.Usage.TotalTokens != draft.Usage.TotalTokens {
		t.Fatalf("planner usage was lost: %#v", normalized.Usage)
	}
}

func TestNormalizePlanDoesNotCollapseShortCodingTask(t *testing.T) {
	draft := PlanDraft{Tasks: []string{"inspect docs", "report contents"}}
	normalized := normalizePlanForObjective("查看 docs 目录", draft)
	if len(normalized.Tasks) != 2 {
		t.Fatalf("coding task was incorrectly collapsed: %#v", normalized.Tasks)
	}
}

func TestParseReplanDecision(t *testing.T) {
	complete, err := ParseReplanDecision("COMPLETE\nDone for the user.")
	if err != nil || complete.Action != ReplanComplete || complete.FinalAnswer != "Done for the user." {
		t.Fatalf("parse complete: %#v, %v", complete, err)
	}
	replan, err := ParseReplanDecision("REPLAN\n- inspect again\n- finish")
	if err != nil || replan.Action != ReplanAgain || len(replan.Plan.Tasks) != 2 {
		t.Fatalf("parse replan: %#v, %v", replan, err)
	}
	if _, err := ParseReplanDecision("A plain final answer"); err == nil {
		t.Fatal("plain replan response unexpectedly parsed")
	}
}

func TestPlannerAndReplannerPublishLLMCallLifecycle(t *testing.T) {
	events := event.NewMemorySink()
	client := &planClient{responses: []llm.Response{
		{ID: "response-plan", RequestID: "request-plan", Message: llm.AssistantMessage("PLAN\n- inspect"), FinishReason: llm.FinishReasonStop, Usage: llm.Usage{TotalTokens: 4}},
		{ID: "response-review", RequestID: "request-review", Message: llm.AssistantMessage("COMPLETE\ndone"), FinishReason: llm.FinishReasonStop, Usage: llm.Usage{TotalTokens: 3}},
	}}
	options := PlannerOptions{MaxAttempts: 1, MaxOutputTokens: 128, Events: events}
	planner, err := NewLLMPlanDraftPlanner(client, options)
	if err != nil {
		t.Fatal(err)
	}
	replanner, err := NewLLMReplanner(client, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx := event.WithMetadata(context.Background(), event.Metadata{SessionID: "session-1", RunID: "run-1"})
	if _, err := planner.Draft(ctx, DraftPlanRequest{Goal: Goal{Objective: "inspect"}}); err != nil {
		t.Fatal(err)
	}
	reviewCtx := event.WithMetadata(ctx, event.Metadata{Iteration: 2})
	if _, err := replanner.Decide(reviewCtx, ReplanRequest{Goal: Goal{Objective: "inspect"}, Graph: ExecutionGraph{Kind: ExecutionPlanned, Version: 1, Tasks: []Task{{ID: "task-1", Objective: "inspect", Status: TaskStatusCompleted}}}}); err != nil {
		t.Fatal(err)
	}

	var started []event.LLMCallStarted
	var completed []event.LLMCallCompleted
	for _, runtimeEvent := range events.Snapshot() {
		switch typed := runtimeEvent.(type) {
		case event.LLMCallStarted:
			started = append(started, typed)
		case event.LLMCallCompleted:
			completed = append(completed, typed)
		}
	}
	if len(started) != 2 || len(completed) != 2 {
		t.Fatalf("unexpected Plan LLM lifecycle events: started=%#v completed=%#v", started, completed)
	}
	if started[0].LLMCallID != "run-1/plan/initial-attempt-1" || started[1].LLMCallID != "run-1/plan/review-cycle-2-attempt-1" {
		t.Fatalf("unexpected Plan LLM call IDs: %#v", started)
	}
	for _, item := range append(started, event.LLMCallStarted{SessionID: completed[0].SessionID, RunID: completed[0].RunID, LLMCallID: completed[0].LLMCallID}) {
		if item.SessionID != "session-1" || item.RunID != "run-1" || item.LLMCallID == "" {
			t.Fatalf("missing Plan LLM metadata: %#v", item)
		}
	}
}

type fixedDraftPlanner struct {
	drafts []PlanDraft
	index  int
}

func (planner *fixedDraftPlanner) Draft(context.Context, DraftPlanRequest) (PlanDraft, error) {
	draft := planner.drafts[planner.index]
	planner.index++
	return draft, nil
}

type fixedReplanner struct {
	decisions []ReplanDecision
	index     int
}

func (replanner *fixedReplanner) Decide(context.Context, ReplanRequest) (ReplanDecision, error) {
	decision := replanner.decisions[replanner.index]
	replanner.index++
	return decision, nil
}

type fixedTaskRunner struct {
	objectives []string
}

type cancelledTaskRunner struct{}

func (cancelledTaskRunner) Run(context.Context, TaskRunInput) (TaskOutcome, error) {
	return TaskOutcome{Kind: TaskOutcomeCancelled, StopReason: StopReasonCancelled, Reason: "user cancelled"}, nil
}

type rejectingReplanner struct{ called bool }

func (replanner *rejectingReplanner) Decide(context.Context, ReplanRequest) (ReplanDecision, error) {
	replanner.called = true
	return ReplanDecision{}, errors.New("replanner must not be called after cancellation")
}

func (runner *fixedTaskRunner) Run(_ context.Context, input TaskRunInput) (TaskOutcome, error) {
	runner.objectives = append(runner.objectives, input.Task.Objective)
	return TaskOutcome{
		Kind: TaskOutcomeCandidateComplete,
		Candidate: &CandidateTaskResult{
			Result:       TaskResult{Summary: "completed " + input.Task.Objective},
			FinalMessage: llm.AssistantMessage("completed " + input.Task.Objective),
		},
		Budget: input.Budget,
	}, nil
}

func TestControllerRunsPlanThenReplans(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"inspect", "report"}}}}
	runner := &fixedTaskRunner{}
	replanner := &fixedReplanner{decisions: []ReplanDecision{
		{Action: ReplanAgain, Plan: PlanDraft{Tasks: []string{"verify"}}},
		{Action: ReplanComplete, FinalAnswer: "all done"},
	}}
	planEngine, err := NewController(planner, runner, replanner, ControllerOptions{MaxPlanCycles: 3})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	goal := Goal{Objective: "complete work"}
	result, err := planEngine.Run(context.Background(), PlanRunInput{State: NewRun("run", goal, NewPlanGraph(), Budget{})})
	if err != nil {
		t.Fatalf("run engine: %v", err)
	}
	if result.State.Status != RunStatusCompleted || result.FinalMessage == nil || result.FinalMessage.Content != "all done" {
		t.Fatalf("unexpected result: %#v", result)
	}
	want := []string{"inspect", "report", "verify"}
	if len(runner.objectives) != len(want) {
		t.Fatalf("objectives: %#v", runner.objectives)
	}
	for index := range want {
		if runner.objectives[index] != want[index] {
			t.Fatalf("objectives: %#v", runner.objectives)
		}
	}
}

func TestControllerPublishesPlanAndTerminalTaskStatuses(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"inspect"}}}}
	runner := &fixedTaskRunner{}
	replanner := &fixedReplanner{decisions: []ReplanDecision{{Action: ReplanComplete, FinalAnswer: "done"}}}
	events := event.NewMemorySink()
	planEngine, err := NewController(planner, runner, replanner, ControllerOptions{Events: events})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	goal := Goal{Objective: "inspect"}
	if _, err := planEngine.Run(context.Background(), PlanRunInput{State: NewRun("run", goal, NewPlanGraph(), Budget{})}); err != nil {
		t.Fatalf("run engine: %v", err)
	}

	var plan event.PlanUpdated
	var statuses []event.RunStatusChanged
	for _, runtimeEvent := range events.Snapshot() {
		switch typed := runtimeEvent.(type) {
		case event.PlanUpdated:
			plan = typed
		case event.RunStatusChanged:
			if typed.Entity == "task" {
				statuses = append(statuses, typed)
			}
		}
	}
	if plan.Cycle != 1 || len(plan.Tasks) != 1 || plan.Tasks[0].ID != "task-1" || plan.Tasks[0].Objective != "inspect" || plan.Tasks[0].Status != string(TaskStatusPending) {
		t.Fatalf("unexpected published plan: %#v", plan)
	}
	if len(statuses) != 2 || statuses[0].TaskID != "task-1" || statuses[0].From != string(TaskStatusPending) || statuses[0].To != string(TaskStatusRunning) || statuses[1].TaskID != "task-1" || statuses[1].From != string(TaskStatusRunning) || statuses[1].To != string(TaskStatusCompleted) {
		t.Fatalf("unexpected task status events: %#v", statuses)
	}
}

func TestControllerPublishesEachReplanCycle(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"first"}}}}
	runner := &fixedTaskRunner{}
	replanner := &fixedReplanner{decisions: []ReplanDecision{
		{Action: ReplanAgain, Plan: PlanDraft{Tasks: []string{"second"}}},
		{Action: ReplanComplete, FinalAnswer: "done"},
	}}
	events := event.NewMemorySink()
	planEngine, err := NewController(planner, runner, replanner, ControllerOptions{Events: events})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	goal := Goal{Objective: "work"}
	if _, err := planEngine.Run(context.Background(), PlanRunInput{State: NewRun("run", goal, NewPlanGraph(), Budget{})}); err != nil {
		t.Fatalf("run engine: %v", err)
	}
	var plans []event.PlanUpdated
	for _, runtimeEvent := range events.Snapshot() {
		if plan, ok := runtimeEvent.(event.PlanUpdated); ok {
			plans = append(plans, plan)
		}
	}
	if len(plans) != 2 || plans[0].Cycle != 1 || plans[0].Tasks[0].Objective != "first" || plans[1].Cycle != 2 || plans[1].Tasks[0].Objective != "second" {
		t.Fatalf("unexpected replan events: %#v", plans)
	}
}

func TestControllerDoesNotReplanAfterCancellation(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"inspect"}}}}
	replanner := &rejectingReplanner{}
	controller, err := NewController(planner, cancelledTaskRunner{}, replanner, ControllerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	goal := Goal{Objective: "inspect"}
	result, err := controller.Run(context.Background(), PlanRunInput{State: NewRun("run", goal, NewPlanGraph(), Budget{})})
	if err != nil {
		t.Fatal(err)
	}
	if replanner.called || result.State.Status != RunStatusCancelled || result.State.StopReason != StopReasonCancelled {
		t.Fatalf("unexpected cancelled Plan result: called=%t result=%#v", replanner.called, result)
	}
}
