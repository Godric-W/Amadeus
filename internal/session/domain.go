package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

type ProjectID string
type ConversationSessionID string
type TurnID string
type RunID string

type ConversationSessionStatus string

const (
	ConversationSessionActive   ConversationSessionStatus = "active"
	ConversationSessionArchived ConversationSessionStatus = "archived"
)

func (status ConversationSessionStatus) Valid() bool {
	return status == ConversationSessionActive || status == ConversationSessionArchived
}

type TurnStatus string

const (
	TurnRunning      TurnStatus = "running"
	TurnCompleted    TurnStatus = "completed"
	TurnCancelled    TurnStatus = "cancelled"
	TurnFailed       TurnStatus = "failed"
	TurnPartial      TurnStatus = "partial"
	TurnNeedsPlan    TurnStatus = "needs_plan"
	TurnAwaitingUser TurnStatus = "awaiting_user"
)

func (status TurnStatus) Valid() bool {
	switch status {
	case TurnRunning, TurnCompleted, TurnCancelled, TurnFailed, TurnPartial, TurnNeedsPlan, TurnAwaitingUser:
		return true
	default:
		return false
	}
}

func (status TurnStatus) Terminal() bool {
	return status.Valid() && status != TurnRunning
}

type RunStatus string

const (
	RunRunning      RunStatus = "running"
	RunCompleted    RunStatus = "completed"
	RunCancelled    RunStatus = "cancelled"
	RunFailed       RunStatus = "failed"
	RunPartial      RunStatus = "partial"
	RunNeedsPlan    RunStatus = "needs_plan"
	RunAwaitingUser RunStatus = "awaiting_user"
)

func (status RunStatus) Valid() bool {
	switch status {
	case RunRunning, RunCompleted, RunCancelled, RunFailed, RunPartial, RunNeedsPlan, RunAwaitingUser:
		return true
	default:
		return false
	}
}

func (status RunStatus) Terminal() bool {
	return status.Valid() && status != RunRunning
}

type Project struct {
	ID            ProjectID `json:"id"`
	CanonicalPath string    `json:"canonical_path"`
	DisplayName   string    `json:"display_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LastOpenedAt  time.Time `json:"last_opened_at"`
}

func NewProject(id ProjectID, canonicalPath, displayName string, now time.Time) (Project, error) {
	now = now.UTC()
	project := Project{
		ID: id, CanonicalPath: strings.TrimSpace(canonicalPath), DisplayName: strings.TrimSpace(displayName),
		CreatedAt: now, UpdatedAt: now, LastOpenedAt: now,
	}
	return project, project.Validate()
}

func (project Project) Validate() error {
	if err := validateID("project", string(project.ID)); err != nil {
		return err
	}
	if project.CanonicalPath == "" || !filepath.IsAbs(project.CanonicalPath) || filepath.Clean(project.CanonicalPath) != project.CanonicalPath {
		return errors.New("project canonical path must be a clean absolute path")
	}
	if strings.TrimSpace(project.DisplayName) == "" {
		return errors.New("project display name is empty")
	}
	if err := validateTimeline(project.CreatedAt, project.UpdatedAt, "project updated_at"); err != nil {
		return err
	}
	return validateTimeline(project.CreatedAt, project.LastOpenedAt, "project last_opened_at")
}

type ConversationSession struct {
	ID               ConversationSessionID     `json:"id"`
	ProjectID        ProjectID                 `json:"project_id"`
	Title            string                    `json:"title"`
	Status           ConversationSessionStatus `json:"status"`
	NextTurnSequence int64                     `json:"next_turn_sequence"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	LastActiveAt     time.Time                 `json:"last_active_at"`
}

func NewConversationSession(id ConversationSessionID, projectID ProjectID, title string, now time.Time) (ConversationSession, error) {
	now = now.UTC()
	session := ConversationSession{
		ID: id, ProjectID: projectID, Title: strings.TrimSpace(title), Status: ConversationSessionActive,
		NextTurnSequence: 1, CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	return session, session.Validate()
}

func (session ConversationSession) Validate() error {
	if err := validateID("conversation session", string(session.ID)); err != nil {
		return err
	}
	if err := validateID("conversation session project", string(session.ProjectID)); err != nil {
		return err
	}
	if strings.TrimSpace(session.Title) == "" {
		return errors.New("conversation session title is empty")
	}
	if !session.Status.Valid() {
		return fmt.Errorf("conversation session status %q is invalid", session.Status)
	}
	if session.NextTurnSequence < 1 {
		return errors.New("conversation session next turn sequence must be at least one")
	}
	if err := validateTimeline(session.CreatedAt, session.UpdatedAt, "conversation session updated_at"); err != nil {
		return err
	}
	return validateTimeline(session.CreatedAt, session.LastActiveAt, "conversation session last_active_at")
}

func (session *ConversationSession) Transition(status ConversationSessionStatus, at time.Time) error {
	if session == nil {
		return errors.New("conversation session is nil")
	}
	if !status.Valid() {
		return fmt.Errorf("conversation session status %q is invalid", status)
	}
	if session.Status == status {
		return &TransitionError{Entity: "conversation_session", ID: string(session.ID), From: string(session.Status), To: string(status)}
	}
	at = at.UTC()
	if at.IsZero() || at.Before(session.UpdatedAt) || at.Before(session.LastActiveAt) {
		return errors.New("conversation session transition time precedes current timestamps")
	}
	session.Status = status
	session.UpdatedAt = at
	session.LastActiveAt = at
	return session.Validate()
}

func (session *ConversationSession) AllocateTurnSequence(at time.Time) (int64, error) {
	if session == nil {
		return 0, errors.New("conversation session is nil")
	}
	if session.NextTurnSequence < 1 || session.NextTurnSequence == math.MaxInt64 {
		return 0, errors.New("conversation session turn sequence is exhausted")
	}
	at = at.UTC()
	if at.IsZero() || at.Before(session.UpdatedAt) || at.Before(session.LastActiveAt) {
		return 0, errors.New("conversation session allocation time precedes current timestamps")
	}
	sequence := session.NextTurnSequence
	session.NextTurnSequence++
	session.UpdatedAt = at
	session.LastActiveAt = at
	return sequence, session.Validate()
}

type Turn struct {
	ID          TurnID                `json:"id"`
	SessionID   ConversationSessionID `json:"session_id"`
	Sequence    int64                 `json:"sequence"`
	Status      TurnStatus            `json:"status"`
	CreatedAt   time.Time             `json:"created_at"`
	CompletedAt *time.Time            `json:"completed_at,omitempty"`
}

func NewTurn(id TurnID, sessionID ConversationSessionID, sequence int64, now time.Time) (Turn, error) {
	turn := Turn{ID: id, SessionID: sessionID, Sequence: sequence, Status: TurnRunning, CreatedAt: now.UTC()}
	return turn, turn.Validate()
}

func (turn Turn) Validate() error {
	if err := validateID("turn", string(turn.ID)); err != nil {
		return err
	}
	if err := validateID("turn session", string(turn.SessionID)); err != nil {
		return err
	}
	if turn.Sequence < 1 {
		return errors.New("turn sequence must be at least one")
	}
	if !turn.Status.Valid() {
		return fmt.Errorf("turn status %q is invalid", turn.Status)
	}
	if turn.CreatedAt.IsZero() {
		return errors.New("turn created_at is zero")
	}
	if turn.Status == TurnRunning {
		if turn.CompletedAt != nil {
			return errors.New("running turn cannot have completed_at")
		}
		return nil
	}
	if turn.CompletedAt == nil || turn.CompletedAt.IsZero() || turn.CompletedAt.Before(turn.CreatedAt) {
		return errors.New("terminal turn requires completed_at at or after created_at")
	}
	return nil
}

func (turn *Turn) Complete(status TurnStatus, at time.Time) error {
	if turn == nil {
		return errors.New("turn is nil")
	}
	if !status.Terminal() || turn.Status != TurnRunning {
		return &TransitionError{Entity: "turn", ID: string(turn.ID), From: string(turn.Status), To: string(status)}
	}
	at = at.UTC()
	if at.IsZero() || at.Before(turn.CreatedAt) {
		return errors.New("turn completion time precedes creation")
	}
	turn.Status = status
	turn.CompletedAt = &at
	return turn.Validate()
}

type Run struct {
	ID                       RunID                 `json:"id"`
	SessionID                ConversationSessionID `json:"session_id"`
	TurnID                   TurnID                `json:"turn_id"`
	ContextFromRunID         RunID                 `json:"context_from_run_id,omitempty"`
	Objective                string                `json:"objective"`
	Status                   RunStatus             `json:"status"`
	StopReason               string                `json:"stop_reason,omitempty"`
	Provider                 string                `json:"provider,omitempty"`
	Model                    string                `json:"model,omitempty"`
	APIMode                  string                `json:"api_mode,omitempty"`
	Dialect                  string                `json:"dialect,omitempty"`
	BudgetJSON               json.RawMessage       `json:"budget_json,omitempty"`
	UsageJSON                json.RawMessage       `json:"usage_json,omitempty"`
	LatestCheckpointSequence int64                 `json:"latest_checkpoint_sequence"`
	StartedAt                time.Time             `json:"started_at"`
	FinishedAt               *time.Time            `json:"finished_at,omitempty"`
}

func NewRun(id RunID, sessionID ConversationSessionID, turnID TurnID, objective string, now time.Time) (Run, error) {
	run := Run{
		ID: id, SessionID: sessionID, TurnID: turnID, Objective: strings.TrimSpace(objective),
		Status: RunRunning, StartedAt: now.UTC(),
	}
	return run, run.Validate()
}

func (run Run) Validate() error {
	if err := validateID("run", string(run.ID)); err != nil {
		return err
	}
	if err := validateID("run session", string(run.SessionID)); err != nil {
		return err
	}
	if err := validateID("run turn", string(run.TurnID)); err != nil {
		return err
	}
	if run.ContextFromRunID != "" {
		if err := validateID("run context", string(run.ContextFromRunID)); err != nil {
			return err
		}
		if run.ContextFromRunID == run.ID {
			return errors.New("run cannot use itself as interrupted context")
		}
	}
	if strings.TrimSpace(run.Objective) == "" {
		return errors.New("run objective is empty")
	}
	if !run.Status.Valid() {
		return fmt.Errorf("run status %q is invalid", run.Status)
	}
	if run.LatestCheckpointSequence < 0 {
		return errors.New("run latest checkpoint sequence cannot be negative")
	}
	if run.StartedAt.IsZero() {
		return errors.New("run started_at is zero")
	}
	if err := validateOptionalJSON("run budget_json", run.BudgetJSON); err != nil {
		return err
	}
	if err := validateOptionalJSON("run usage_json", run.UsageJSON); err != nil {
		return err
	}
	if run.Status == RunRunning {
		if run.FinishedAt != nil || strings.TrimSpace(run.StopReason) != "" {
			return errors.New("running run cannot have finished_at or stop reason")
		}
		return nil
	}
	if run.FinishedAt == nil || run.FinishedAt.IsZero() || run.FinishedAt.Before(run.StartedAt) {
		return errors.New("terminal run requires finished_at at or after started_at")
	}
	if run.Status != RunCompleted && strings.TrimSpace(run.StopReason) == "" {
		return errors.New("non-completed terminal run requires a stop reason")
	}
	return nil
}

func (run *Run) Finish(status RunStatus, stopReason string, at time.Time) error {
	if run == nil {
		return errors.New("run is nil")
	}
	if !status.Terminal() || run.Status != RunRunning {
		return &TransitionError{Entity: "run", ID: string(run.ID), From: string(run.Status), To: string(status)}
	}
	at = at.UTC()
	if at.IsZero() || at.Before(run.StartedAt) {
		return errors.New("run finish time precedes start")
	}
	run.Status = status
	run.StopReason = strings.TrimSpace(stopReason)
	run.FinishedAt = &at
	return run.Validate()
}

type TransitionError struct {
	Entity string
	ID     string
	From   string
	To     string
}

func (err *TransitionError) Error() string {
	return fmt.Sprintf("invalid %s transition for %q: %s -> %s", err.Entity, err.ID, err.From, err.To)
}

func validateID(entity, id string) error {
	if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, " \t\r\n") {
		return fmt.Errorf("%s ID is empty or contains whitespace", entity)
	}
	return nil
}

func validateTimeline(createdAt, value time.Time, name string) error {
	if createdAt.IsZero() {
		return errors.New("created_at is zero")
	}
	if value.IsZero() || value.Before(createdAt) {
		return fmt.Errorf("%s must be at or after created_at", name)
	}
	return nil
}

func validateOptionalJSON(name string, content json.RawMessage) error {
	if len(content) != 0 && !json.Valid(content) {
		return fmt.Errorf("%s is invalid JSON", name)
	}
	return nil
}
