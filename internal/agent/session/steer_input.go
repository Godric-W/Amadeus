package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type SteerInputErrorKind string

const (
	SteerInputNoActiveTurn         SteerInputErrorKind = "no_active_turn"
	SteerInputExpectedTurnMismatch SteerInputErrorKind = "expected_turn_mismatch"
	SteerInputActiveNotSteerable   SteerInputErrorKind = "active_turn_not_steerable"
	SteerInputEmpty                SteerInputErrorKind = "empty_input"
)

type SteerInputError struct {
	Kind     SteerInputErrorKind
	Expected protocol.TurnID
	Actual   protocol.TurnID
	TaskKind TaskKind
}

func (err *SteerInputError) Error() string {
	if err == nil {
		return ""
	}
	switch err.Kind {
	case SteerInputNoActiveTurn:
		return "no active turn"
	case SteerInputExpectedTurnMismatch:
		return fmt.Sprintf("expected active turn %q, got %q", err.Expected, err.Actual)
	case SteerInputActiveNotSteerable:
		return fmt.Sprintf("active %s turn is not steerable", err.TaskKind)
	case SteerInputEmpty:
		return "steer input is empty"
	default:
		return "steer input failed"
	}
}

func isSteerInputError(err error, kind SteerInputErrorKind) bool {
	var steerErr *SteerInputError
	return errors.As(err, &steerErr) && steerErr.Kind == kind
}

type steerInputRequest struct {
	input          UserTurnInput
	expectedTurnID protocol.TurnID
	result         chan steerInputResult
}

type steerInputResult struct {
	turnID protocol.TurnID
	err    error
}

func (io SessionIo) SteerInput(ctx context.Context, expectedTurnID protocol.TurnID, input UserTurnInput) (protocol.TurnID, error) {
	if ctx == nil {
		return "", errors.New("steer input context is nil")
	}
	if strings.TrimSpace(string(expectedTurnID)) == "" {
		return "", errors.New("expected turn ID is empty")
	}
	if err := input.validate(); err != nil {
		return "", err
	}
	if io.steerRequests == nil || io.Terminated == nil {
		return "", errors.New("session steer input is unavailable")
	}
	result := make(chan steerInputResult, 1)
	request := steerInputRequest{input: input, expectedTurnID: expectedTurnID, result: result}
	select {
	case io.steerRequests <- request:
	case <-io.Terminated:
		return "", errors.New("session is terminated")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case response := <-result:
		return response.turnID, response.err
	case <-io.Terminated:
		return "", errors.New("session is terminated")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (session *Session) steerInput(input UserTurnInput, expectedTurnID protocol.TurnID) (protocol.TurnID, error) {
	if err := input.validate(); err != nil {
		return "", &SteerInputError{Kind: SteerInputEmpty}
	}
	if session.active == nil || session.active.Task == nil {
		return "", &SteerInputError{Kind: SteerInputNoActiveTurn}
	}
	actualTurnID := session.active.Task.Context().TurnID
	if expectedTurnID != "" && expectedTurnID != actualTurnID {
		return "", &SteerInputError{Kind: SteerInputExpectedTurnMismatch, Expected: expectedTurnID, Actual: actualTurnID}
	}
	if session.active.Task.Kind() != TaskKindRegular {
		return "", &SteerInputError{Kind: SteerInputActiveNotSteerable, TaskKind: session.active.Task.Kind(), Actual: actualTurnID}
	}
	if err := session.inputQueue.Enqueue(session.active.State, input); err != nil {
		if errors.Is(err, errTurnInputQueueSealed) {
			return "", &SteerInputError{Kind: SteerInputNoActiveTurn}
		}
		return "", err
	}
	return actualTurnID, nil
}
