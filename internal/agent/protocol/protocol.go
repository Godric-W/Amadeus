package protocol

import (
	"time"

	"github.com/Godric-W/Amadeus/internal/rollout"
)

type Submission struct {
	Op Op
}

type Op interface {
	isOp()
}

type UserInputOp struct {
	Content string
}

func (UserInputOp) isOp() {}

type CompactOp struct{}

func (CompactOp) isOp() {}

type InterruptOp struct{}

func (InterruptOp) isOp() {}

type ShutdownOp struct{}

func (ShutdownOp) isOp() {}

type ApprovalDecisionOp struct {
	RequestID string
	Decision  string
}

func (ApprovalDecisionOp) isOp() {}

type UserInputResponseOp struct {
	RequestID string
	Content   string
}

func (UserInputResponseOp) isOp() {}

type ThreadSettingsOp struct {
	PermissionMode string
}

func (ThreadSettingsOp) isOp() {}

type SessionEvent struct {
	ThreadID rollout.ThreadID
	TurnID   rollout.TurnID
	Message  EventMessage
}

type EventMessage interface {
	isEventMessage()
}

type ThreadConfigured struct{}

func (ThreadConfigured) isEventMessage() {}

type TurnStarted struct {
	StartedAt time.Time
}

func (TurnStarted) isEventMessage() {}

type TurnRejected struct {
	Error      string
	RejectedAt time.Time
}

func (TurnRejected) isEventMessage() {}

type TurnCompleted struct {
	Status     rollout.TurnTerminalStatus
	Error      string
	FinishedAt time.Time
}

func (TurnCompleted) isEventMessage() {}

type TurnAborted struct {
	Reason     string
	FinishedAt time.Time
}

func (TurnAborted) isEventMessage() {}

type Warning struct {
	Message string
}

func (Warning) isEventMessage() {}

type StreamError struct {
	Error string
}

func (StreamError) isEventMessage() {}

type InteractiveRequest struct {
	RequestID string
	ThreadID  rollout.ThreadID
	TurnID    rollout.TurnID
	Kind      string
	Payload   any
}

type AgentStatus struct {
	ThreadID rollout.ThreadID
	TurnID   rollout.TurnID
	Working  bool
}
