package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type Submission struct{ Op Op }

type Op interface{ isOp() }

type UserInputOp struct{ Content string }

func (UserInputOp) isOp() {}

type CompactOp struct{}

func (CompactOp) isOp() {}

type InterruptOp struct{}

func (InterruptOp) isOp() {}

type ShutdownOp struct{}

func (ShutdownOp) isOp() {}

type ApprovalDecisionOp struct {
	RequestID string
	OptionID  string
	Outcome   string
	Scope     string
	Source    string
	Reason    string
}

func (ApprovalDecisionOp) isOp() {}

type UserInputResponseOp struct {
	RequestID string
	Content   string
}

func (UserInputResponseOp) isOp() {}

type ThreadSettingsOp struct{ PermissionMode string }

func (ThreadSettingsOp) isOp() {}

type SessionEvent struct {
	ThreadID rollout.ThreadID
	TurnID   rollout.TurnID
	Message  EventMessage
}

func (event SessionEvent) Validate() error {
	if event.ThreadID == "" {
		return errors.New("session event thread ID is empty")
	}
	if event.Message == nil {
		return errors.New("session event message is nil")
	}
	return nil
}

type EventMessage interface{ isEventMessage() }

type ThreadConfigured struct{}

func (ThreadConfigured) isEventMessage() {}

type TurnStarted struct {
	StartedAt time.Time
	Input     string
}

func (TurnStarted) isEventMessage() {}

type TurnRejected struct {
	Error      string
	RejectedAt time.Time
}

func (TurnRejected) isEventMessage() {}

type TurnCompleted struct {
	Status     rollout.TurnTerminalStatus
	Summary    string
	Error      string
	FinishedAt time.Time
}

func (TurnCompleted) isEventMessage() {}

type TurnAborted struct {
	Summary    string
	Reason     string
	FinishedAt time.Time
}

func (TurnAborted) isEventMessage() {}

type Warning struct{ Message string }

func (Warning) isEventMessage() {}

type StreamError struct{ Error string }

func (StreamError) isEventMessage() {}

type InteractiveRequestKind string

const (
	RequestApproval  InteractiveRequestKind = "approval"
	RequestUserInput InteractiveRequestKind = "user_input"
)

func (kind InteractiveRequestKind) Valid() bool {
	return kind == RequestApproval || kind == RequestUserInput
}

type ApprovalPresentation struct {
	Title       string
	Description string
	Details     []string
	Options     []ApprovalOption
	Diff        *filechange.Preview
}

type ApprovalOption struct {
	ID          string
	Label       string
	Description string
}

type ApprovalRequest struct {
	ID           string
	ToolName     string
	Presentation ApprovalPresentation
	Raw          json.RawMessage
}

type UserInputRequest struct {
	ID     string
	Prompt string
	Secret bool
}

type InteractiveRequest struct {
	RequestID string
	ThreadID  rollout.ThreadID
	TurnID    rollout.TurnID
	Kind      InteractiveRequestKind
	Approval  *ApprovalRequest
	UserInput *UserInputRequest
}

func (request InteractiveRequest) Validate() error {
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("interactive request ID is empty")
	}
	if !request.Kind.Valid() {
		return fmt.Errorf("interactive request kind %q is invalid", request.Kind)
	}
	if request.Kind == RequestApproval && request.Approval == nil {
		return errors.New("approval request payload is nil")
	}
	if request.Kind == RequestUserInput && request.UserInput == nil {
		return errors.New("user input request payload is nil")
	}
	return nil
}

type AgentStatus struct {
	ThreadID rollout.ThreadID
	TurnID   rollout.TurnID
	Working  bool
}
