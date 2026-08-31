package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) handleSubmission(submission protocol.Submission) {
	if err := submission.Validate(); err != nil {
		session.admissions.complete(submission.ID, userMessageAdmissionResult{err: err})
		if _, userInput := submission.Op.(protocol.UserInputOp); !userInput {
			session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ErrorEvent{ThreadID: session.threadID, Code: "invalid_submission", Message: err.Error(), At: session.services.Clock().UTC()}})
		}
		return
	}
	switch op := submission.Op.(type) {
	case protocol.UserInputOp:
		admission, err := session.admitUserMessage(submission.ID, op)
		session.admissions.complete(submission.ID, userMessageAdmissionResult{admission: admission, err: err})
		if err != nil {
			var settingsErr *settingsValidationError
			if errors.As(err, &settingsErr) {
				session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ErrorEvent{
					ThreadID: session.threadID, Code: "invalid_thread_settings", Message: err.Error(), At: session.services.Clock().UTC(),
				}})
			}
		}
	case protocol.CompactOp:
		if session.active != nil {
			session.deferred = append(session.deferred, submission)
			return
		}
		_, _ = session.startTurn(submission.ID, "compact context", "", TaskKindCompact, nil)
	case protocol.InterruptOp:
		session.cancelActive(ErrInterrupted)
	case protocol.ThreadSettingsOp:
		if session.active != nil {
			session.deferred = append(session.deferred, submission)
			return
		}
		if !op.Mode.Valid() {
			session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ErrorEvent{
				ThreadID: session.threadID, Code: "invalid_thread_settings", Message: fmt.Sprintf("collaboration mode %q is invalid", op.Mode), At: session.services.Clock().UTC(),
			}})
			return
		}
		session.applyMode(submission.ID, ModeKind(op.Mode))
	case protocol.ApprovalDecisionOp:
		session.resolveRequest(op.RequestID, op)
	case protocol.UserInputAnswerOp:
		session.resolveRequest(op.RequestID, op)
	}
}

func (session *Session) admitUserMessage(submissionID protocol.SubmissionID, op protocol.UserInputOp) (protocol.UserMessageAdmission, error) {
	content := op.Content
	if content == "" {
		return protocol.UserMessageAdmission{}, errors.New("user input is empty")
	}
	mode, err := prepareModeOverride(op.ThreadSettings)
	if err != nil {
		return protocol.UserMessageAdmission{}, err
	}
	turnID, err := session.steerInput(UserTurnInput{Content: content, ClientID: strings.TrimSpace(op.ClientUserMessageID)}, "")
	if err == nil {
		if mode != nil {
			session.applyMode(submissionID, *mode)
		}
		return protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionSteered, TurnID: turnID}, nil
	}
	if !isSteerInputError(err, SteerInputNoActiveTurn) {
		return protocol.UserMessageAdmission{}, err
	}
	turnID, err = session.startTurn(submissionID, content, strings.TrimSpace(op.ClientUserMessageID), TaskKindRegular, mode)
	if err != nil {
		return protocol.UserMessageAdmission{}, err
	}
	return protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionStarted, TurnID: turnID}, nil
}

type settingsValidationError struct{ err error }

func (err *settingsValidationError) Error() string {
	if err == nil || err.err == nil {
		return "thread settings are invalid"
	}
	return err.err.Error()
}

func (err *settingsValidationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.err
}

func prepareModeOverride(overrides protocol.ThreadSettingsOverrides) (*ModeKind, error) {
	if overrides.CollaborationMode == nil {
		return nil, nil
	}
	mode := overrides.CollaborationMode.Mode
	if !mode.Valid() {
		return nil, &settingsValidationError{err: fmt.Errorf("collaboration mode %q is invalid", mode)}
	}
	value := ModeKind(mode)
	return &value, nil
}
