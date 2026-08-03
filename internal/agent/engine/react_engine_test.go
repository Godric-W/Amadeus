package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type reactEngineRunner struct {
	outcome TaskOutcome
	inputs  []TaskRunInput
}

func (runner *reactEngineRunner) Run(_ context.Context, input TaskRunInput) (TaskOutcome, error) {
	runner.inputs = append(runner.inputs, input)
	return runner.outcome, nil
}

func TestReActEngineCompletesWithoutPlanner(t *testing.T) {
	runner := &reactEngineRunner{outcome: TaskOutcome{
		Kind: TaskOutcomeCandidateComplete,
		Candidate: &CandidateTaskResult{
			Result:       TaskResult{Summary: "你好！"},
			FinalMessage: llm.AssistantMessage("你好！"),
		},
		Budget: BudgetState{},
	}}
	sink := event.NewMemorySink()
	reactEngine, err := NewReActEngine(runner, sink)
	if err != nil {
		t.Fatal(err)
	}
	goal := Goal{Objective: "你好"}
	result, err := reactEngine.Run(context.Background(), DirectRunInput{
		State:    NewRun("run-react", goal, NewDirectGraph(goal), Budget{}),
		Messages: []llm.Message{llm.UserMessage("你好")},
	})
	if err != nil {
		t.Fatalf("run ReAct engine: %v", err)
	}
	if len(runner.inputs) != 1 || result.State.Status != RunStatusCompleted || result.FinalMessage == nil || result.FinalMessage.Content != "你好！" {
		t.Fatalf("unexpected ReAct result: inputs=%d result=%#v", len(runner.inputs), result)
	}
	for _, item := range sink.Snapshot() {
		if _, planned := item.(event.PlanUpdated); planned {
			t.Fatalf("default ReAct engine published a plan: %#v", sink.Snapshot())
		}
	}
}

func TestReActEngineDoesNotAutoUpgradeToPlan(t *testing.T) {
	runner := &reactEngineRunner{outcome: TaskOutcome{Kind: TaskOutcomeNeedsPlan, Reason: "multiple dependent changes", Budget: BudgetState{}}}
	reactEngine, err := NewReActEngine(runner, event.NewMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	goal := Goal{Objective: "refactor project"}
	result, err := reactEngine.Run(context.Background(), DirectRunInput{State: NewRun("run-react", goal, NewDirectGraph(goal), Budget{})})
	if err != nil {
		t.Fatalf("run ReAct engine: %v", err)
	}
	if result.State.Status != RunStatusSuspended || !strings.Contains(result.Reason, "/plan <task>") {
		t.Fatalf("unexpected needs-plan result: %#v", result)
	}
}
