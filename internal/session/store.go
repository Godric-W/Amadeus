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

type BeginFirstTurnInput struct {
	ProjectID        ProjectID
	CanonicalPath    string
	ProjectName      string
	SessionID        ConversationSessionID
	SessionTitle     string
	TurnID           TurnID
	UserMessageID    MessageID
	RunID            RunID
	Objective        string
	UserContent      string
	ContextFromRunID RunID
	Provider         string
	Model            string
	APIMode          string
	Dialect          string
	BudgetJSON       json.RawMessage
	StartedAt        time.Time
}

type BeginTurnInput struct {
	SessionID        ConversationSessionID
	TurnID           TurnID
	UserMessageID    MessageID
	RunID            RunID
	Objective        string
	UserContent      string
	ContextFromRunID RunID
	Provider         string
	Model            string
	APIMode          string
	Dialect          string
	BudgetJSON       json.RawMessage
	StartedAt        time.Time
}

type BeginTurnResult struct {
	Project Project
	Session ConversationSession
	Turn    Turn
	Message Message
	Run     Run
}

type FinishTurnInput struct {
	SessionID          ConversationSessionID
	TurnID             TurnID
	RunID              RunID
	TurnStatus         TurnStatus
	RunStatus          RunStatus
	StopReason         string
	AssistantMessageID MessageID
	AssistantContent   string
	UsageJSON          json.RawMessage
	FinishedAt         time.Time
}

type FinishTurnResult struct {
	Session          ConversationSession
	Turn             Turn
	Run              Run
	AssistantMessage *Message
}

type AppendCheckpointInput struct {
	Checkpoint   Checkpoint
	Instructions []CheckpointInstruction
}

type SessionStore interface {
	GetProjectByCanonicalPath(context.Context, string) (Project, error)
	GetSession(context.Context, ConversationSessionID) (ConversationSession, error)
	ListSessions(context.Context, ProjectID) ([]ConversationSession, error)
	LatestSession(context.Context, ProjectID) (ConversationSession, error)
}

type ConversationStore interface {
	BeginFirstTurn(context.Context, BeginFirstTurnInput) (BeginTurnResult, error)
	BeginTurn(context.Context, BeginTurnInput) (BeginTurnResult, error)
	FinishTurn(context.Context, FinishTurnInput) (FinishTurnResult, error)
	ListMessages(context.Context, ConversationSessionID) ([]Message, error)
}

type RunStore interface {
	GetRun(context.Context, RunID) (Run, error)
	LatestInterruptedRun(context.Context, ConversationSessionID) (Run, error)
	PendingInterruptedRun(context.Context, ConversationSessionID) (Run, error)
}

type CheckpointStore interface {
	AppendCheckpoint(context.Context, AppendCheckpointInput) (Checkpoint, error)
	ListCheckpoints(context.Context, RunID) ([]Checkpoint, error)
	ListCheckpointInstructions(context.Context, CheckpointID) ([]CheckpointInstruction, error)
}

type SummaryStore interface {
	AppendSummary(context.Context, ConversationSummary) (ConversationSummary, error)
	LatestSummary(context.Context, ConversationSessionID) (ConversationSummary, error)
}

type Store interface {
	SessionStore
	ConversationStore
	RunStore
	CheckpointStore
	SummaryStore
}
