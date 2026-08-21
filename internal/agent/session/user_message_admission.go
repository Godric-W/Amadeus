package session

import (
	"context"
	"errors"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type userMessageAdmissionResult struct {
	admission protocol.UserMessageAdmission
	err       error
}

type pendingUserMessageAdmissions struct {
	mu      sync.Mutex
	pending map[protocol.SubmissionID]chan userMessageAdmissionResult
}

func newPendingUserMessageAdmissions() *pendingUserMessageAdmissions {
	return &pendingUserMessageAdmissions{pending: make(map[protocol.SubmissionID]chan userMessageAdmissionResult)}
}

func (admissions *pendingUserMessageAdmissions) register(submissionID protocol.SubmissionID) (<-chan userMessageAdmissionResult, func()) {
	result := make(chan userMessageAdmissionResult, 1)
	admissions.mu.Lock()
	admissions.pending[submissionID] = result
	admissions.mu.Unlock()
	return result, func() { admissions.remove(submissionID) }
}

func (admissions *pendingUserMessageAdmissions) complete(submissionID protocol.SubmissionID, result userMessageAdmissionResult) {
	admissions.mu.Lock()
	waiter := admissions.pending[submissionID]
	delete(admissions.pending, submissionID)
	admissions.mu.Unlock()
	if waiter != nil {
		waiter <- result
	}
}

func (admissions *pendingUserMessageAdmissions) remove(submissionID protocol.SubmissionID) {
	admissions.mu.Lock()
	delete(admissions.pending, submissionID)
	admissions.mu.Unlock()
}

func (admissions *pendingUserMessageAdmissions) failAll(err error) {
	if err == nil {
		err = errors.New("session terminated before user message admission")
	}
	admissions.mu.Lock()
	pending := admissions.pending
	admissions.pending = make(map[protocol.SubmissionID]chan userMessageAdmissionResult)
	admissions.mu.Unlock()
	for _, waiter := range pending {
		waiter <- userMessageAdmissionResult{err: err}
	}
}

func (io SessionIo) SubmitUserInputAndWaitForAdmission(ctx context.Context, submission protocol.Submission) (protocol.UserMessageAdmission, error) {
	if ctx == nil {
		return protocol.UserMessageAdmission{}, errors.New("user message admission context is nil")
	}
	if _, ok := submission.Op.(protocol.UserInputOp); !ok {
		return protocol.UserMessageAdmission{}, errors.New("user message admission requires UserInputOp")
	}
	if err := submission.Validate(); err != nil {
		return protocol.UserMessageAdmission{}, err
	}
	if io.admissions == nil || io.Submissions == nil || io.Terminated == nil {
		return protocol.UserMessageAdmission{}, errors.New("session user message admission is unavailable")
	}
	result, unregister := io.admissions.register(submission.ID)
	defer unregister()
	select {
	case io.Submissions <- submission:
	case <-io.Terminated:
		return protocol.UserMessageAdmission{}, errors.New("session is terminated")
	case <-ctx.Done():
		return protocol.UserMessageAdmission{}, ctx.Err()
	}
	select {
	case admitted := <-result:
		if admitted.err != nil {
			return protocol.UserMessageAdmission{}, admitted.err
		}
		if err := admitted.admission.Validate(); err != nil {
			return protocol.UserMessageAdmission{}, err
		}
		return admitted.admission, nil
	case <-io.Terminated:
		return protocol.UserMessageAdmission{}, errors.New("session is terminated")
	case <-ctx.Done():
		return protocol.UserMessageAdmission{}, ctx.Err()
	}
}
