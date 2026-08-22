package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol/identity"
	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type ThreadID = identity.ThreadID

type TurnID = identity.TurnID

type SubmissionID = identity.SubmissionID

type RequestID = identity.RequestID

type ItemID = identity.ItemID

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

type ApprovalDecisionOp struct {
	RequestID RequestID
	OptionID  string
	Outcome   string
	Scope     string
	Source    string
	Reason    string
}

func (ApprovalDecisionOp) isOp() {}

type ThreadSettingsOp struct{ Mode ModeKind }

func (ThreadSettingsOp) isOp() {}

type Event struct {
	ID  SubmissionID
	Msg EventMsg
}

func (event Event) Validate() error {
	if strings.TrimSpace(string(event.ID)) == "" {
		return errors.New("event correlation ID is empty")
	}
	if event.Msg == nil {
		return errors.New("event message is nil")
	}
	if configured, ok := event.Msg.(SessionConfiguredEvent); ok {
		return configured.Validate()
	}
	return nil
}

type EventMsg interface{ isEventMsg() }

type SessionConfiguration struct {
	Source          SessionSource
	CWD             string
	Provider        string
	Model           string
	ReasoningEffort *llm.ReasoningEffort `json:"reasoning_effort,omitempty"`
	Mode            ModeKind
}

func (configuration SessionConfiguration) Clone() SessionConfiguration {
	configuration.Source = configuration.Source.Clone()
	configuration.ReasoningEffort = llm.CloneReasoningEffort(configuration.ReasoningEffort)
	return configuration
}

type SessionConfiguredEvent struct {
	SessionID      SessionID
	ThreadID       ThreadID
	ParentThreadID *ThreadID
	Configuration  SessionConfiguration
}

func (SessionConfiguredEvent) isEventMsg() {}

func (event SessionConfiguredEvent) Validate() error {
	if event.SessionID.IsZero() || event.ThreadID.IsZero() {
		return errors.New("session configured identity is incomplete")
	}
	if err := event.Configuration.Source.Validate(); err != nil {
		return err
	}
	if event.Configuration.Source.IsSubAgent() {
		if event.ParentThreadID == nil || event.ParentThreadID.IsZero() || *event.ParentThreadID != event.Configuration.Source.SubAgent.ParentThreadID {
			return errors.New("sub-agent configured parent identity is inconsistent")
		}
		if event.ThreadID == *event.ParentThreadID {
			return errors.New("sub-agent configured thread cannot be its own parent")
		}
		return nil
	}
	if event.ParentThreadID != nil {
		return errors.New("root configured event has parent thread ID")
	}
	if event.SessionID != SessionIDFromThreadID(event.ThreadID) {
		return errors.New("root configured session ID does not match thread ID")
	}
	return nil
}

type ThreadSettingsAppliedEvent struct {
	ThreadID      ThreadID
	Configuration SessionConfiguration
}

func (ThreadSettingsAppliedEvent) isEventMsg() {}

type ShutdownCompleteEvent struct{ ThreadID ThreadID }

func (ShutdownCompleteEvent) isEventMsg() {}

type TurnStartedEvent struct {
	ThreadID  ThreadID
	TurnID    TurnID
	StartedAt time.Time
}

func (TurnStartedEvent) isEventMsg() {}

type ErrorEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	Code     string
	Message  string
	At       time.Time
}

func (ErrorEvent) isEventMsg() {}

type TurnTerminalStatus string

const (
	TurnStatusCompleted TurnTerminalStatus = "completed"
	TurnStatusFailed    TurnTerminalStatus = "failed"
)

type TurnOutcome string

const (
	TurnOutcomeCompleted TurnOutcome = "completed"
	TurnOutcomeBlocked   TurnOutcome = "blocked"
	TurnOutcomeFailed    TurnOutcome = "failed"
	TurnOutcomeAborted   TurnOutcome = "aborted"
)

func (outcome TurnOutcome) Valid() bool {
	switch outcome {
	case TurnOutcomeCompleted, TurnOutcomeBlocked, TurnOutcomeFailed, TurnOutcomeAborted:
		return true
	default:
		return false
	}
}

type TurnCompleteEvent struct {
	ThreadID   ThreadID
	TurnID     TurnID
	Status     TurnTerminalStatus
	Outcome    TurnOutcome
	Reason     string
	Summary    string
	Error      string
	FinishedAt time.Time
}

func (TurnCompleteEvent) isEventMsg() {}

type TurnAbortedEvent struct {
	ThreadID   ThreadID
	TurnID     TurnID
	Summary    string
	Reason     string
	FinishedAt time.Time
}

func (TurnAbortedEvent) isEventMsg() {}

type WarningEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	Message  string
}

func (WarningEvent) isEventMsg() {}

type ProviderErrorInfo struct {
	Kind       string
	Code       string
	StatusCode int
	RequestID  string
	Provider   string
	Retryable  bool
	RetryDelay time.Duration
}

type StreamErrorEvent struct {
	ThreadID          ThreadID
	TurnID            TurnID
	Message           string
	AdditionalDetails *string
	ProviderError     *ProviderErrorInfo
	WillRetry         bool
}

func (StreamErrorEvent) isEventMsg() {}

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
	ID           RequestID
	ToolName     string
	Presentation ApprovalPresentation
	Raw          json.RawMessage
}

type ApprovalRequestEvent struct {
	RequestID RequestID
	ThreadID  ThreadID
	TurnID    TurnID
	Approval  ApprovalRequest
}

func (ApprovalRequestEvent) isEventMsg() {}

func (request ApprovalRequestEvent) Validate() error {
	if strings.TrimSpace(string(request.RequestID)) == "" {
		return errors.New("approval request ID is empty")
	}
	if strings.TrimSpace(string(request.Approval.ID)) == "" {
		return errors.New("approval request payload is incomplete")
	}
	return nil
}

func ScopeEventMsg(message EventMsg, threadID ThreadID, turnID TurnID) EventMsg {
	switch value := message.(type) {
	case SessionConfiguredEvent:
		value.ThreadID = threadID
		return value
	case ThreadSettingsAppliedEvent:
		value.ThreadID = threadID
		return value
	case ThreadNameUpdatedEvent:
		value.ThreadID = threadID
		return value
	case ThreadArchivedEvent:
		value.ThreadID = threadID
		return value
	case ShutdownCompleteEvent:
		value.ThreadID = threadID
		return value
	case TurnStartedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ErrorEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TurnCompleteEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TurnAbortedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case WarningEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case StreamErrorEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ApprovalRequestEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case RequestUserInputEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ContextUpdateEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case SubagentNotificationEvent:
		value.ThreadID = threadID
		return value
	default:
		return ScopeItemEventMsg(message, threadID, turnID)
	}
}

func ThreadIDOf(message EventMsg) ThreadID {
	switch value := message.(type) {
	case SessionConfiguredEvent:
		return value.ThreadID
	case ThreadSettingsAppliedEvent:
		return value.ThreadID
	case ThreadNameUpdatedEvent:
		return value.ThreadID
	case ThreadArchivedEvent:
		return value.ThreadID
	case ShutdownCompleteEvent:
		return value.ThreadID
	case TurnStartedEvent:
		return value.ThreadID
	case ErrorEvent:
		return value.ThreadID
	case TurnCompleteEvent:
		return value.ThreadID
	case TurnAbortedEvent:
		return value.ThreadID
	case WarningEvent:
		return value.ThreadID
	case StreamErrorEvent:
		return value.ThreadID
	case ApprovalRequestEvent:
		return value.ThreadID
	case RequestUserInputEvent:
		return value.ThreadID
	case ContextUpdateEvent:
		return value.ThreadID
	case SubagentNotificationEvent:
		return value.ThreadID
	default:
		return ItemEventThreadID(message)
	}
}

func TurnIDOf(message EventMsg) TurnID {
	switch value := message.(type) {
	case TurnStartedEvent:
		return value.TurnID
	case ErrorEvent:
		return value.TurnID
	case TurnCompleteEvent:
		return value.TurnID
	case TurnAbortedEvent:
		return value.TurnID
	case WarningEvent:
		return value.TurnID
	case StreamErrorEvent:
		return value.TurnID
	case ApprovalRequestEvent:
		return value.TurnID
	case RequestUserInputEvent:
		return value.TurnID
	case ContextUpdateEvent:
		return value.TurnID
	default:
		return ItemEventTurnID(message)
	}
}
