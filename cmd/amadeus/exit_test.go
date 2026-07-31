package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
)

func TestClassifyRunResultMapsStableOutcomes(t *testing.T) {
	tests := []struct {
		status  engine.RunStatus
		outcome runOutcome
		code    int
	}{
		{engine.RunStatusCompleted, runOutcomeCompleted, exitCodeSuccess},
		{engine.RunStatusSuspended, runOutcomePartial, exitCodePartial},
		{engine.RunStatusFailed, runOutcomeFailed, exitCodeFailure},
		{engine.RunStatusCancelled, runOutcomeCancelled, exitCodeCancelled},
		{engine.RunStatusPlanning, runOutcomeNeedsPlan, exitCodeNeedsPlan},
	}

	for _, test := range tests {
		outcome, code, err := classifyRunResult(engine.DirectRunResult{State: engine.RunState{Status: test.status}})
		if err != nil || outcome != test.outcome || code != test.code {
			t.Fatalf("status %q mapped to outcome=%q code=%d err=%v", test.status, outcome, code, err)
		}
	}
}

func TestClassifyRunResultRejectsNonTerminalStatus(t *testing.T) {
	_, _, err := classifyRunResult(engine.DirectRunResult{State: engine.RunState{Status: engine.RunStatusTaskRunning}})
	if err == nil || !strings.Contains(err.Error(), "task_running") {
		t.Fatalf("unexpected non-terminal status error: %v", err)
	}
}

func TestCommandExitErrorControlsProcessSemantics(t *testing.T) {
	err := &commandExitError{code: exitCodeNeedsPlan, message: "result: needs_plan", reported: true}
	if exitCode(err) != exitCodeNeedsPlan || !errorAlreadyReported(err) {
		t.Fatalf("unexpected command exit semantics: code=%d reported=%t", exitCode(err), errorAlreadyReported(err))
	}
	if exitCode(errors.New("ordinary")) != exitCodeFailure || errorAlreadyReported(errors.New("ordinary")) {
		t.Fatal("ordinary errors must use unreported failure semantics")
	}
}

func TestFormatRunSummaryFoldsAndBoundsReason(t *testing.T) {
	result := engine.DirectRunResult{
		State:  engine.RunState{Status: engine.RunStatusFailed, StopReason: engine.StopReasonProviderError},
		Reason: strings.Repeat("line\n", 100),
	}
	summary := formatRunSummary(runOutcomeFailed, result)
	if strings.Contains(summary, "\n") || len(summary) > 340 || !strings.Contains(summary, "stop_reason=provider_error") {
		t.Fatalf("unsafe Run summary: %q", summary)
	}
}
