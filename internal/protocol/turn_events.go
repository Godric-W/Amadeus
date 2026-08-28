package protocol

import "time"

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
	ThreadID         ThreadID
	TurnID           TurnID
	Status           TurnTerminalStatus
	Outcome          TurnOutcome
	Reason           string
	Summary          string
	Error            string
	LastAgentMessage *string
	FinishedAt       time.Time
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
