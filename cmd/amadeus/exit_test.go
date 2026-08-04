package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
)

func TestClassifyRunResultMapsStableOutcomes(t *testing.T) {
	tests := []struct {
		status  plan.RunStatus
		outcome runOutcome
		code    int
	}{
		{plan.RunStatusCompleted, runOutcomeCompleted, exitCodeSuccess},
		{plan.RunStatusFailed, runOutcomeFailed, exitCodeFailure},
		{plan.RunStatusCancelled, runOutcomeCancelled, exitCodeCancelled},
	}

	for _, test := range tests {
		outcome, code, err := classifyRunResult(plan.PlanRunResult{State: plan.RunState{Status: test.status}})
		if err != nil || outcome != test.outcome || code != test.code {
			t.Fatalf("status %q mapped to outcome=%q code=%d err=%v", test.status, outcome, code, err)
		}
	}
}

func TestClassifyRunResultRejectsNonTerminalStatus(t *testing.T) {
	_, _, err := classifyRunResult(plan.PlanRunResult{State: plan.RunState{Status: plan.RunStatusTaskRunning}})
	if err == nil || !strings.Contains(err.Error(), "task_running") {
		t.Fatalf("unexpected non-terminal status error: %v", err)
	}
}

func TestCommandExitErrorControlsProcessSemantics(t *testing.T) {
	err := &commandExitError{code: exitCodePartial, message: "result: partial", reported: true}
	if exitCode(err) != exitCodePartial || !errorAlreadyReported(err) {
		t.Fatalf("unexpected command exit semantics: code=%d reported=%t", exitCode(err), errorAlreadyReported(err))
	}
	if exitCode(errors.New("ordinary")) != exitCodeFailure || errorAlreadyReported(errors.New("ordinary")) {
		t.Fatal("ordinary errors must use unreported failure semantics")
	}
}

func TestFormatRunSummaryFoldsAndBoundsReason(t *testing.T) {
	result := plan.PlanRunResult{
		State:  plan.RunState{Status: plan.RunStatusFailed, StopReason: plan.StopReasonProviderError},
		Reason: strings.Repeat("line\n", 100),
	}
	summary := formatRunSummary(runOutcomeFailed, result)
	if strings.Contains(summary, "\n") || len(summary) > 340 || !strings.Contains(summary, "stop_reason=provider_error") {
		t.Fatalf("unsafe Run summary: %q", summary)
	}
}
