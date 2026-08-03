package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

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

func TestPlanExecuteEngineRunsPlanThenReplans(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"inspect", "report"}}}}
	runner := &fixedTaskRunner{}
	replanner := &fixedReplanner{decisions: []ReplanDecision{
		{Action: ReplanAgain, Plan: PlanDraft{Tasks: []string{"verify"}}},
		{Action: ReplanComplete, FinalAnswer: "all done"},
	}}
	planEngine, err := NewPlanExecuteEngine(planner, runner, replanner, PlanExecuteEngineOptions{MaxPlanCycles: 3})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	goal := Goal{Objective: "complete work"}
	result, err := planEngine.Run(context.Background(), DirectRunInput{State: NewRun("run", goal, NewDirectGraph(goal), Budget{})})
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

func TestPlanExecuteEnginePublishesPlanAndTerminalTaskStatuses(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"inspect"}}}}
	runner := &fixedTaskRunner{}
	replanner := &fixedReplanner{decisions: []ReplanDecision{{Action: ReplanComplete, FinalAnswer: "done"}}}
	events := event.NewMemorySink()
	planEngine, err := NewPlanExecuteEngine(planner, runner, replanner, PlanExecuteEngineOptions{Events: events})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	goal := Goal{Objective: "inspect"}
	if _, err := planEngine.Run(context.Background(), DirectRunInput{State: NewRun("run", goal, NewDirectGraph(goal), Budget{})}); err != nil {
		t.Fatalf("run engine: %v", err)
	}

	var plan event.PlanUpdated
	var statuses []event.EngineStatusChanged
	for _, runtimeEvent := range events.Snapshot() {
		switch typed := runtimeEvent.(type) {
		case event.PlanUpdated:
			plan = typed
		case event.EngineStatusChanged:
			if typed.Entity == "task" {
				statuses = append(statuses, typed)
			}
		}
	}
	if plan.Cycle != 1 || len(plan.Tasks) != 1 || plan.Tasks[0].ID != "task-1" || plan.Tasks[0].Objective != "inspect" || plan.Tasks[0].Status != string(TaskStatusPending) {
		t.Fatalf("unexpected published plan: %#v", plan)
	}
	if len(statuses) != 2 || statuses[0].From != string(TaskStatusPending) || statuses[0].To != string(TaskStatusRunning) || statuses[1].From != string(TaskStatusRunning) || statuses[1].To != string(TaskStatusCompleted) {
		t.Fatalf("unexpected task status events: %#v", statuses)
	}
}

func TestPlanExecuteEnginePublishesEachReplanCycle(t *testing.T) {
	planner := &fixedDraftPlanner{drafts: []PlanDraft{{Tasks: []string{"first"}}}}
	runner := &fixedTaskRunner{}
	replanner := &fixedReplanner{decisions: []ReplanDecision{
		{Action: ReplanAgain, Plan: PlanDraft{Tasks: []string{"second"}}},
		{Action: ReplanComplete, FinalAnswer: "done"},
	}}
	events := event.NewMemorySink()
	planEngine, err := NewPlanExecuteEngine(planner, runner, replanner, PlanExecuteEngineOptions{Events: events})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	goal := Goal{Objective: "work"}
	if _, err := planEngine.Run(context.Background(), DirectRunInput{State: NewRun("run", goal, NewDirectGraph(goal), Budget{})}); err != nil {
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
