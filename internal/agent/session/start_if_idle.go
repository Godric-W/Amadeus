package session

import (
	"context"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type NotSubmittedReason string

const (
	NotSubmittedNotIdle                NotSubmittedReason = "not_idle"
	NotSubmittedPlanMode               NotSubmittedReason = "plan_mode"
	NotSubmittedPendingTriggerTurn     NotSubmittedReason = "pending_trigger_turn"
	NotSubmittedEmptyInput             NotSubmittedReason = "empty_input"
	NotSubmittedPersistentThreadNeeded NotSubmittedReason = "persistent_thread_required"
)

type StartIfIdleSubmission struct {
	TurnID protocol.TurnID
	Reason NotSubmittedReason
}

func (submission StartIfIdleSubmission) Started() bool { return submission.TurnID != "" }

type startIfIdleRequest struct {
	input  TurnInput
	result chan startIfIdleResult
}

type startIfIdleResult struct {
	submission StartIfIdleSubmission
	err        error
}

func (io SessionIo) StartTurnIfIdle(ctx context.Context, input TurnInput) (StartIfIdleSubmission, error) {
	if ctx == nil {
		return StartIfIdleSubmission{}, errors.New("start-if-idle context is nil")
	}
	if input == nil {
		return StartIfIdleSubmission{Reason: NotSubmittedEmptyInput}, nil
	}
	if io.startIfIdleRequests == nil || io.Terminated == nil {
		return StartIfIdleSubmission{}, errors.New("start-if-idle is unavailable")
	}
	result := make(chan startIfIdleResult, 1)
	request := startIfIdleRequest{input: input, result: result}
	select {
	case io.startIfIdleRequests <- request:
	case <-io.Terminated:
		return StartIfIdleSubmission{}, errors.New("session is terminated")
	case <-ctx.Done():
		return StartIfIdleSubmission{}, ctx.Err()
	}
	select {
	case response := <-result:
		return response.submission, response.err
	case <-io.Terminated:
		return StartIfIdleSubmission{}, errors.New("session is terminated")
	case <-ctx.Done():
		return StartIfIdleSubmission{}, ctx.Err()
	}
}

func (session *Session) startTurnIfIdle(input TurnInput) (StartIfIdleSubmission, error) {
	if input == nil {
		return StartIfIdleSubmission{Reason: NotSubmittedEmptyInput}, nil
	}
	if session.active != nil {
		return StartIfIdleSubmission{Reason: NotSubmittedNotIdle}, nil
	}
	if len(session.deferred) > 0 || len(session.submissions) > 0 {
		return StartIfIdleSubmission{Reason: NotSubmittedPendingTriggerTurn}, nil
	}
	if response, ok := input.(ResponseItemTurnInput); ok {
		if err := response.validate(); err != nil {
			return StartIfIdleSubmission{}, err
		}
		if session.Configuration().Mode == ModeKindPlan {
			return StartIfIdleSubmission{Reason: NotSubmittedPlanMode}, nil
		}
		if !session.services.LiveThread.IsMaterialized() {
			return StartIfIdleSubmission{Reason: NotSubmittedPersistentThreadNeeded}, nil
		}
	}
	if user, ok := input.(UserTurnInput); ok {
		if err := user.validate(); err != nil {
			return StartIfIdleSubmission{Reason: NotSubmittedEmptyInput}, nil
		}
	}
	title := "automatic continuation"
	if user, ok := input.(UserTurnInput); ok {
		title = user.Content
	}
	submissionID := protocol.SubmissionID(session.services.NextID("automatic-turn"))
	if strings.TrimSpace(string(submissionID)) == "" {
		return StartIfIdleSubmission{}, errors.New("automatic turn submission ID is empty")
	}
	turnID, err := session.startTurn(submissionID, title, input, TaskKindRegular, nil)
	if err != nil {
		return StartIfIdleSubmission{}, err
	}
	return StartIfIdleSubmission{TurnID: turnID}, nil
}
