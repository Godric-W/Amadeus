package cli

import (
	"context"
	"errors"
)

const (
	exitCodeSuccess   = 0
	exitCodeFailure   = 1
	exitCodePartial   = 2
	exitCodeCancelled = 130
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

func (err *commandExitError) ExitCode() int {
	return err.code
}

func (err *commandExitError) AlreadyReported() bool {
	return err.reported
}

type exitCoder interface {
	ExitCode() int
}

type reportedError interface {
	AlreadyReported() bool
}

func exitCode(err error) int {
	if err == nil {
		return exitCodeSuccess
	}
	var exitErr exitCoder
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if errors.Is(err, context.Canceled) {
		return exitCodeCancelled
	}
	return exitCodeFailure
}

func errorAlreadyReported(err error) bool {
	var exitErr reportedError
	return errors.As(err, &exitErr) && exitErr.AlreadyReported()
}
