package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
)

const (
	exitCodeSuccess   = 0
	exitCodeFailure   = 1
	exitCodePartial   = 2
	exitCodeCancelled = 130
)

type runOutcome string

const (
	runOutcomeCompleted runOutcome = "completed"
	runOutcomePartial   runOutcome = "partial"
	runOutcomeFailed    runOutcome = "failed"
	runOutcomeCancelled runOutcome = "cancelled"
)

type commandExitError struct {
	code     int
	message  string
	reported bool
}

func (err *commandExitError) Error() string {
	return err.message
}

func exitCode(err error) int {
	if err == nil {
		return exitCodeSuccess
	}
	var exitErr *commandExitError
	if errors.As(err, &exitErr) {
		return exitErr.code
	}
	return exitCodeFailure
}

func errorAlreadyReported(err error) bool {
	var exitErr *commandExitError
	return errors.As(err, &exitErr) && exitErr.reported
}

func classifyRunResult(result plan.PlanRunResult) (runOutcome, int, error) {
	switch result.State.Status {
	case plan.RunStatusCompleted:
		return runOutcomeCompleted, exitCodeSuccess, nil
	case plan.RunStatusCancelled:
		return runOutcomeCancelled, exitCodeCancelled, nil
	case plan.RunStatusFailed:
		return runOutcomeFailed, exitCodeFailure, nil
	default:
		return "", exitCodeFailure, fmt.Errorf("unsupported terminal Run status %q", result.State.Status)
	}
}

func formatRunSummary(outcome runOutcome, result plan.PlanRunResult) string {
	parts := []string{"result: " + string(outcome)}
	if result.State.StopReason != "" {
		parts = append(parts, "stop_reason="+string(result.State.StopReason))
	}
	if reason := strings.TrimSpace(result.Reason); reason != "" {
		parts = append(parts, "reason="+foldSummary(reason))
	}
	return strings.Join(parts, " ")
}

func foldSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maximum = 256
	if len(value) <= maximum {
		return value
	}
	return value[:maximum-3] + "..."
}
