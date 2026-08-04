package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("session record not found")
	ErrConflict = errors.New("session record conflict")
)

type BeginFirstRunInput struct {
	ProjectID        ProjectID
	CanonicalPath    string
	ProjectName      string
	SessionID        ConversationSessionID
	SessionTitle     string
	UserMessageID    MessageID
	RunID            RunID
	Objective        string
	UserContent      string
	ContextFromRunID RunID
	Provider         string
	Model            string
	APIMode          string
	Dialect          string
	ExecutionMode    ExecutionMode
	StartedAt        time.Time
}

type BeginRunInput struct {
	SessionID        ConversationSessionID
	UserMessageID    MessageID
	RunID            RunID
	Objective        string
	UserContent      string
	ContextFromRunID RunID
	Provider         string
	Model            string
	APIMode          string
	Dialect          string
	ExecutionMode    ExecutionMode
	StartedAt        time.Time
}

type BeginRunResult struct {
	Project Project
	Session ConversationSession
	Message Message
	Run     Run
}

type FinishRunInput struct {
	SessionID          ConversationSessionID
	RunID              RunID
	RunStatus          RunStatus
	StopReason         string
	AssistantMessageID MessageID
	AssistantContent   string
	UsageJSON          json.RawMessage
	InterruptedContext json.RawMessage
	FinishedAt         time.Time
}

type FinishRunResult struct {
	Session          ConversationSession
	Run              Run
	AssistantMessage *Message
}

type SessionStore interface {
	GetProjectByCanonicalPath(context.Context, string) (Project, error)
	GetSession(context.Context, ConversationSessionID) (ConversationSession, error)
	ListSessions(context.Context, ProjectID) ([]ConversationSession, error)
	LatestSession(context.Context, ProjectID) (ConversationSession, error)
}

type ConversationStore interface {
	BeginFirstRun(context.Context, BeginFirstRunInput) (BeginRunResult, error)
	BeginRun(context.Context, BeginRunInput) (BeginRunResult, error)
	FinishRun(context.Context, FinishRunInput) (FinishRunResult, error)
	ListCompletedMessages(context.Context, ConversationSessionID) ([]Message, error)
}

type RunStore interface {
	GetRun(context.Context, RunID) (Run, error)
	LatestInterruptedRun(context.Context, ConversationSessionID) (Run, error)
	PendingInterruptedRun(context.Context, ConversationSessionID) (Run, error)
	RecoverRunningRuns(context.Context, ConversationSessionID, time.Time) error
}

type SummaryStore interface {
	AppendSummary(context.Context, ConversationSummary) (ConversationSummary, error)
	LatestSummary(context.Context, ConversationSessionID) (ConversationSummary, error)
}

type Store interface {
	SessionStore
	ConversationStore
	RunStore
	SummaryStore
}
