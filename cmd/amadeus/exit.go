package main

import (
	"errors"
	"strings"
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
	cause    error
}

func (err *commandExitError) Error() string {
	return err.message
}

func (err *commandExitError) Unwrap() error {
	return err.cause
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

func foldSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maximum = 256
	if len(value) <= maximum {
		return value
	}
	return value[:maximum-3] + "..."
}
