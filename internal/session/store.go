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
	ProjectID     ProjectID
	CanonicalPath string
	ProjectName   string
	SessionID     SessionID
	SessionTitle  string
	RunID         RunID
	UserItemID    RolloutItemID
	UserContent   string
	Provider      string
	Model         string
	APIMode       string
	Dialect       string
	Mode          RunMode
	StartedAt     time.Time
}

type BeginRunInput struct {
	SessionID   SessionID
	RunID       RunID
	UserItemID  RolloutItemID
	UserContent string
	Provider    string
	Model       string
	APIMode     string
	Dialect     string
	Mode        RunMode
	StartedAt   time.Time
}

type BeginRunResult struct {
	Project Project
	Session Session
	Run     Run
	Item    RolloutItem
}

type AppendItem struct {
	ID        RolloutItemID
	RunID     RunID
	Kind      RolloutKind
	Payload   json.RawMessage
	CreatedAt time.Time
}

type AppendItemsInput struct {
	SessionID SessionID
	Items     []AppendItem
}

type FinishRunInput struct {
	SessionID     SessionID
	RunID         RunID
	RunStatus     RunStatus
	StopReason    string
	UsageJSON     json.RawMessage
	TerminalItems []AppendItem
	FinishedAt    time.Time
}

type FinishRunResult struct {
	Session Session
	Run     Run
	Items   []RolloutItem
}

type Store interface {
	GetProjectByCanonicalPath(context.Context, string) (Project, error)
	GetSession(context.Context, SessionID) (Session, error)
	ListSessions(context.Context, ProjectID) ([]Session, error)
	LatestSession(context.Context, ProjectID) (Session, error)

	BeginFirstRun(context.Context, BeginFirstRunInput) (BeginRunResult, error)
	BeginRun(context.Context, BeginRunInput) (BeginRunResult, error)
	AppendItems(context.Context, AppendItemsInput) ([]RolloutItem, error)
	FinishRun(context.Context, FinishRunInput) (FinishRunResult, error)

	GetRun(context.Context, RunID) (Run, error)
	ListItems(context.Context, SessionID) ([]RolloutItem, error)
	RecoverRunningRuns(context.Context, SessionID, time.Time) error
}
