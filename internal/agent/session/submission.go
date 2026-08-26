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
	case protocol.CompactOp:
		if session.active != nil {
			session.deferred = append(session.deferred, submission)
			return
		}
		_, _ = session.startTurn(submission.ID, "compact context", "", TaskKindCompact)
	case protocol.InterruptOp:
		session.cancelActive(ErrInterrupted)
	case protocol.ThreadSettingsOp:
		if session.active != nil {
			session.deferred = append(session.deferred, submission)
			return
		}
		if op.Mode.Valid() {
			session.setMode(ModeKind(op.Mode))
			session.publish(protocol.Event{ID: submission.ID, Msg: protocol.ThreadSettingsAppliedEvent{
				ThreadID: session.threadID, Configuration: session.ProtocolConfiguration(),
			}})
		}
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
	if op.ThreadSettings.CollaborationMode != nil {
		mode := op.ThreadSettings.CollaborationMode.Mode
		if !mode.Valid() {
			return protocol.UserMessageAdmission{}, fmt.Errorf("collaboration mode %q is invalid", mode)
		}
		session.setMode(ModeKind(mode))
		session.publish(protocol.Event{ID: submissionID, Msg: protocol.ThreadSettingsAppliedEvent{
			ThreadID: session.threadID, Configuration: session.ProtocolConfiguration(),
		}})
	}
	turnID, err := session.steerInput(UserTurnInput{Content: content, ClientID: strings.TrimSpace(op.ClientUserMessageID)}, "")
	if err == nil {
		return protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionSteered, TurnID: turnID}, nil
	}
	if !isSteerInputError(err, SteerInputNoActiveTurn) {
		return protocol.UserMessageAdmission{}, err
	}
	turnID, err = session.startTurn(submissionID, content, strings.TrimSpace(op.ClientUserMessageID), TaskKindRegular)
	if err != nil {
		return protocol.UserMessageAdmission{}, err
	}
	return protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionStarted, TurnID: turnID}, nil
}
