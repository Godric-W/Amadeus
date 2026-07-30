package engine

import (
	"errors"
	"testing"
)

func TestRunStateAllowsDirectAndPlannedPaths(t *testing.T) {
	direct := NewRun(RunID("direct"), Goal{Objective: "answer"}, NewDirectGraph(Goal{Objective: "answer"}), Budget{})
	for _, next := range []RunStatus{
		RunStatusStrategySelecting,
		RunStatusTaskRunning,
		RunStatusVerifyingTask,
		RunStatusReflectingTask,
		RunStatusScheduling,
		RunStatusVerifyingRun,
		RunStatusReflectingFinal,
		RunStatusSynthesizing,
		RunStatusCompleted,
	} {
		if err := direct.Transition(next); err != nil {
			t.Fatalf("transition direct run to %q: %v", next, err)
		}
	}

	planned := NewRun(RunID("planned"), Goal{Objective: "change"}, ExecutionGraph{Kind: ExecutionPlanned, Version: 1}, Budget{})
	for _, next := range []RunStatus{RunStatusStrategySelecting, RunStatusPlanning, RunStatusPlanReady, RunStatusScheduling, RunStatusTaskRunning} {
		if err := planned.Transition(next); err != nil {
			t.Fatalf("transition planned run to %q: %v", next, err)
		}
	}
}

func TestTaskCandidateCompletionRequiresVerificationAndReflection(t *testing.T) {
	task := Task{Status: TaskStatusRunning}
	if err := task.Transition(TaskStatusCandidateComplete); err != nil {
		t.Fatalf("transition to candidate complete: %v", err)
	}
	err := task.Transition(TaskStatusCompleted)
	var transitionError *TransitionError
	if !errors.As(err, &transitionError) {
		t.Fatalf("candidate completed without verification: %v", err)
	}
	for _, next := range []TaskStatus{TaskStatusVerifying, TaskStatusReflecting, TaskStatusCompleted} {
		if err := task.Transition(next); err != nil {
			t.Fatalf("transition task to %q: %v", next, err)
		}
	}
}

func TestTerminalStatusesRejectFurtherTransitions(t *testing.T) {
	run := RunState{Status: RunStatusCompleted}
	if err := run.Transition(RunStatusTaskRunning); err == nil {
		t.Fatal("completed run accepted another transition")
	}
	task := Task{Status: TaskStatusFailed}
	if err := task.Transition(TaskStatusReady); err == nil {
		t.Fatal("failed task accepted retry transition")
	}
}

func TestStatusAndStopReasonValidation(t *testing.T) {
	if !RunStatusInitialized.Valid() || RunStatus("unknown").Valid() {
		t.Fatal("unexpected run status validation")
	}
	if !TaskStatusCandidateComplete.Valid() || TaskStatus("unknown").Valid() {
		t.Fatal("unexpected task status validation")
	}
	if !StopReasonVerificationFailed.Valid() || StopReason("unknown").Valid() {
		t.Fatal("unexpected stop reason validation")
	}
}
