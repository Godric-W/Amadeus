package plan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeReActRunner struct {
	input   TaskRunInput
	outcome TaskOutcome
}

func (runner *fakeReActRunner) Run(_ context.Context, input TaskRunInput) (TaskOutcome, error) {
	runner.input = input
	return runner.outcome, nil
}

func TestReActRunnerContractUsesStructuredTaskOutcome(t *testing.T) {
	input := validTaskRunInput()
	expected := TaskOutcome{
		Kind:      TaskOutcomeCandidateComplete,
		Candidate: &CandidateTaskResult{Result: TaskResult{Summary: "candidate", EvidenceIDs: []EvidenceID{"evidence_1"}}},
	}
	runner := &fakeReActRunner{outcome: expected}
	var contract ReActRunner = runner

	outcome, err := contract.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if runner.input.RunID != input.RunID || outcome.Kind != TaskOutcomeCandidateComplete || outcome.Candidate.Result.Summary != "candidate" {
		t.Fatalf("unexpected runner contract values: input=%#v outcome=%#v", runner.input, outcome)
	}
	if err := outcome.Validate(); err != nil {
		t.Fatalf("validate candidate outcome: %v", err)
	}
}

func TestTaskOutcomeKindsValidateTheirRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		outcome TaskOutcome
		valid   bool
	}{
		{name: "candidate", outcome: TaskOutcome{Kind: TaskOutcomeCandidateComplete, Candidate: &CandidateTaskResult{Result: TaskResult{Summary: "done"}}}, valid: true},
		{name: "blocked", outcome: TaskOutcome{Kind: TaskOutcomeBlocked, StopReason: StopReasonUserInputRequired, Reason: "approval required"}, valid: true},
		{name: "failed", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonProviderError, Reason: "provider unavailable"}, valid: true},
		{name: "max steps", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonMaxIterations, Limit: &LimitReached{Limit: BudgetLimitIterations, Used: 3, Maximum: 3}}, valid: true},
		{name: "token budget", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonBudgetExceeded, Limit: &LimitReached{Limit: BudgetLimitInputTokens, Used: 11, Maximum: 10}}, valid: true},
		{name: "cancelled", outcome: TaskOutcome{Kind: TaskOutcomeCancelled, StopReason: StopReasonCancelled}, valid: true},
		{name: "candidate missing result", outcome: TaskOutcome{Kind: TaskOutcomeCandidateComplete}},
		{name: "blocked wrong stop reason", outcome: TaskOutcome{Kind: TaskOutcomeBlocked, StopReason: StopReasonToolError, Reason: "blocked"}},
		{name: "failed completed", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonCompleted}},
		{name: "max steps missing detail", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonMaxIterations}},
		{name: "budget missing detail", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonBudgetExceeded}},
		{name: "provider with limit", outcome: TaskOutcome{Kind: TaskOutcomeFailed, StopReason: StopReasonProviderError, Limit: &LimitReached{Limit: BudgetLimitInputTokens, Used: 1, Maximum: 1}}},
		{name: "cancelled wrong reason", outcome: TaskOutcome{Kind: TaskOutcomeCancelled, StopReason: StopReasonMaxIterations}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.outcome.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid outcome: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid outcome")
			}
		})
	}
}

func TestTaskRunInputRequiresRunningTask(t *testing.T) {
	input := validTaskRunInput()
	if err := input.Validate(); err != nil {
		t.Fatalf("validate task run input: %v", err)
	}
	input.Task.Status = TaskStatusPending
	if err := input.Validate(); err == nil || !strings.Contains(err.Error(), string(TaskStatusRunning)) {
		t.Fatalf("unexpected task status validation: %v", err)
	}
}

func TestTaskOutcomeJSONUsesStableKind(t *testing.T) {
	outcome := TaskOutcome{Kind: TaskOutcomeBlocked, StopReason: StopReasonUserInputRequired, Reason: "approval required"}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("marshal outcome: %v", err)
	}
	if !strings.Contains(string(encoded), `"kind":"blocked"`) {
		t.Fatalf("unexpected outcome JSON: %s", encoded)
	}
}

func validTaskRunInput() TaskRunInput {
	return TaskRunInput{
		RunID: "run_1",
		Task: Task{
			ID:        "task_1",
			Objective: "inspect the repository",
			Status:    TaskStatusRunning,
		},
	}
}
