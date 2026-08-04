package plan

import (
	"errors"
	"testing"
)

func TestRunStateAllowsPlanLifecycle(t *testing.T) {
	planned := NewRun(RunID("planned"), Goal{Objective: "change"}, NewPlanGraph(), Budget{})
	for _, next := range []RunStatus{RunStatusPlanning, RunStatusScheduling, RunStatusTaskRunning, RunStatusCompleted} {
		if err := planned.Transition(next); err != nil {
			t.Fatalf("transition planned run to %q: %v", next, err)
		}
	}
}

func TestPlanTaskLifecycle(t *testing.T) {
	task := Task{Status: TaskStatusPending}
	for _, next := range []TaskStatus{TaskStatusRunning, TaskStatusCompleted} {
		if err := task.Transition(next); err != nil {
			t.Fatalf("transition task to %q: %v", next, err)
		}
	}
	err := task.Transition(TaskStatusRunning)
	var transitionError *TransitionError
	if !errors.As(err, &transitionError) {
		t.Fatalf("completed task accepted another transition: %v", err)
	}
}

func TestStatusAndStopReasonValidation(t *testing.T) {
	if !RunStatusInitialized.Valid() || RunStatus("unknown").Valid() {
		t.Fatal("unexpected run status validation")
	}
	if !TaskStatusRunning.Valid() || TaskStatus("unknown").Valid() {
		t.Fatal("unexpected task status validation")
	}
	if !StopReasonProviderError.Valid() || StopReason("unknown").Valid() {
		t.Fatal("unexpected stop reason validation")
	}
}
