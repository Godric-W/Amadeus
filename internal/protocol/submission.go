package protocol

import (
	"errors"
	"strings"
)

type Submission struct {
	ID SubmissionID
	Op Op
}

func (submission Submission) Validate() error {
	if strings.TrimSpace(string(submission.ID)) == "" {
		return errors.New("submission ID is empty")
	}
	if submission.Op == nil {
		return errors.New("submission op is nil")
	}
	return nil
}

type Op interface{ isOp() }

type UserInputOp struct {
	Content             string
	ClientUserMessageID string
	ThreadSettings      ThreadSettingsOverrides
}

func (UserInputOp) isOp() {}

type CompactOp struct{}

func (CompactOp) isOp() {}

type InterruptOp struct{}

func (InterruptOp) isOp() {}

type ShutdownOp struct{}

func (ShutdownOp) isOp() {}

type ThreadSettingsOp struct{ Mode ModeKind }

func (ThreadSettingsOp) isOp() {}
