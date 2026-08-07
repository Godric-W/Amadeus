package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ProjectID string
type SessionID string
type RunID string
type RolloutItemID string

type SessionStatus string

const (
	SessionActive   SessionStatus = "active"
	SessionArchived SessionStatus = "archived"
)

func (status SessionStatus) Valid() bool {
	return status == SessionActive || status == SessionArchived
}

type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunCompleted   RunStatus = "completed"
	RunInterrupted RunStatus = "interrupted"
	RunFailed      RunStatus = "failed"
)

func (status RunStatus) Valid() bool {
	switch status {
	case RunRunning, RunCompleted, RunInterrupted, RunFailed:
		return true
	default:
		return false
	}
}

func (status RunStatus) Terminal() bool {
	return status.Valid() && status != RunRunning
}

type RunMode string

const (
	RunModeExecute RunMode = "execute"
	RunModePlan    RunMode = "plan"
)

func (mode RunMode) Valid() bool {
	return mode == RunModeExecute || mode == RunModePlan
}

type RolloutKind string

const (
	RolloutUserMessage       RolloutKind = "user_message"
	RolloutAssistantMessage  RolloutKind = "assistant_message"
	RolloutToolCall          RolloutKind = "tool_call"
	RolloutToolResult        RolloutKind = "tool_result"
	RolloutPlanUpdate        RolloutKind = "plan_update"
	RolloutContextSnapshot   RolloutKind = "context_snapshot"
	RolloutRunInterrupted    RolloutKind = "run_interrupted"
	RolloutRunFailed         RolloutKind = "run_failed"
	RolloutContextCompaction RolloutKind = "context_compaction"
)

func (kind RolloutKind) Valid() bool {
	switch kind {
	case RolloutUserMessage, RolloutAssistantMessage, RolloutToolCall, RolloutToolResult,
		RolloutPlanUpdate, RolloutContextSnapshot, RolloutRunInterrupted, RolloutRunFailed,
		RolloutContextCompaction:
		return true
	default:
		return false
	}
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
	value := Project{
		ID: id, CanonicalPath: strings.TrimSpace(canonicalPath), DisplayName: strings.TrimSpace(displayName),
		CreatedAt: now, UpdatedAt: now, LastOpenedAt: now,
	}
	return value, value.Validate()
}

func (value Project) Validate() error {
	if err := validateID("project", string(value.ID)); err != nil {
		return err
	}
	if value.CanonicalPath == "" || !filepath.IsAbs(value.CanonicalPath) || filepath.Clean(value.CanonicalPath) != value.CanonicalPath {
		return errors.New("project canonical path must be a clean absolute path")
	}
	if strings.TrimSpace(value.DisplayName) == "" {
		return errors.New("project display name is empty")
	}
	if err := validateTimeline(value.CreatedAt, value.UpdatedAt, "project updated_at"); err != nil {
		return err
	}
	return validateTimeline(value.CreatedAt, value.LastOpenedAt, "project last_opened_at")
}

type Session struct {
	ID               SessionID     `json:"id"`
	ProjectID        ProjectID     `json:"project_id"`
	Title            string        `json:"title"`
	Status           SessionStatus `json:"status"`
	NextRunSequence  int64         `json:"next_run_sequence"`
	NextItemSequence int64         `json:"next_item_sequence"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	LastActiveAt     time.Time     `json:"last_active_at"`
}

func NewSession(id SessionID, projectID ProjectID, title string, now time.Time) (Session, error) {
	now = now.UTC()
	value := Session{
		ID: id, ProjectID: projectID, Title: strings.TrimSpace(title), Status: SessionActive,
		NextRunSequence: 1, NextItemSequence: 1, CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	return value, value.Validate()
}

func (value Session) Validate() error {
	if err := validateID("session", string(value.ID)); err != nil {
		return err
	}
	if err := validateID("session project", string(value.ProjectID)); err != nil {
		return err
	}
	if strings.TrimSpace(value.Title) == "" {
		return errors.New("session title is empty")
	}
	if !value.Status.Valid() {
		return fmt.Errorf("session status %q is invalid", value.Status)
	}
	if value.NextRunSequence < 1 || value.NextItemSequence < 1 {
		return errors.New("session next sequences must be at least one")
	}
	if err := validateTimeline(value.CreatedAt, value.UpdatedAt, "session updated_at"); err != nil {
		return err
	}
	return validateTimeline(value.CreatedAt, value.LastActiveAt, "session last_active_at")
}

type Run struct {
	ID         RunID           `json:"id"`
	SessionID  SessionID       `json:"session_id"`
	Sequence   int64           `json:"sequence"`
	Status     RunStatus       `json:"status"`
	StopReason string          `json:"stop_reason,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	Model      string          `json:"model,omitempty"`
	APIMode    string          `json:"api_mode,omitempty"`
	Dialect    string          `json:"dialect,omitempty"`
	Mode       RunMode         `json:"run_mode"`
	UsageJSON  json.RawMessage `json:"usage_json,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

func NewRun(id RunID, sessionID SessionID, sequence int64, mode RunMode, now time.Time) (Run, error) {
	value := Run{ID: id, SessionID: sessionID, Sequence: sequence, Status: RunRunning, Mode: mode, StartedAt: now.UTC()}
	return value, value.Validate()
}

func (value Run) Validate() error {
	if err := validateID("run", string(value.ID)); err != nil {
		return err
	}
	if err := validateID("run session", string(value.SessionID)); err != nil {
		return err
	}
	if value.Sequence < 1 {
		return errors.New("run sequence must be at least one")
	}
	if !value.Status.Valid() {
		return fmt.Errorf("run status %q is invalid", value.Status)
	}
	if !value.Mode.Valid() {
		return fmt.Errorf("run mode %q is invalid", value.Mode)
	}
	if value.StartedAt.IsZero() {
		return errors.New("run started_at is zero")
	}
	if err := validateOptionalJSON("run usage_json", value.UsageJSON); err != nil {
		return err
	}
	if value.Status == RunRunning {
		if value.FinishedAt != nil || strings.TrimSpace(value.StopReason) != "" {
			return errors.New("running run cannot have finished_at or stop reason")
		}
		return nil
	}
	if value.FinishedAt == nil || value.FinishedAt.IsZero() || value.FinishedAt.Before(value.StartedAt) {
		return errors.New("terminal run requires finished_at at or after started_at")
	}
	if value.Status != RunCompleted && strings.TrimSpace(value.StopReason) == "" {
		return errors.New("non-completed terminal run requires a stop reason")
	}
	return nil
}

func (value *Run) Finish(status RunStatus, stopReason string, usage json.RawMessage, at time.Time) error {
	if value == nil {
		return errors.New("run is nil")
	}
	if !status.Terminal() || value.Status != RunRunning {
		return &TransitionError{Entity: "run", ID: string(value.ID), From: string(value.Status), To: string(status)}
	}
	at = at.UTC()
	if at.IsZero() || at.Before(value.StartedAt) {
		return errors.New("run finish time precedes start")
	}
	value.Status = status
	value.StopReason = strings.TrimSpace(stopReason)
	value.UsageJSON = append(json.RawMessage(nil), usage...)
	value.FinishedAt = &at
	return value.Validate()
}

type RolloutItem struct {
	ID          RolloutItemID   `json:"id"`
	SessionID   SessionID       `json:"session_id"`
	RunID       RunID           `json:"run_id,omitempty"`
	Sequence    int64           `json:"sequence"`
	Kind        RolloutKind     `json:"kind"`
	PayloadJSON json.RawMessage `json:"payload_json"`
	CreatedAt   time.Time       `json:"created_at"`
}

func NewRolloutItem(id RolloutItemID, sessionID SessionID, runID RunID, sequence int64, kind RolloutKind, payload json.RawMessage, now time.Time) (RolloutItem, error) {
	value := RolloutItem{
		ID: id, SessionID: sessionID, RunID: runID, Sequence: sequence, Kind: kind,
		PayloadJSON: append(json.RawMessage(nil), payload...), CreatedAt: now.UTC(),
	}
	return value, value.Validate()
}

func (value RolloutItem) Validate() error {
	if err := validateID("rollout item", string(value.ID)); err != nil {
		return err
	}
	if err := validateID("rollout item session", string(value.SessionID)); err != nil {
		return err
	}
	if value.RunID != "" {
		if err := validateID("rollout item run", string(value.RunID)); err != nil {
			return err
		}
	}
	if value.Sequence < 1 {
		return errors.New("rollout item sequence must be at least one")
	}
	if !value.Kind.Valid() {
		return fmt.Errorf("rollout item kind %q is invalid", value.Kind)
	}
	if len(value.PayloadJSON) == 0 || !json.Valid(value.PayloadJSON) {
		return errors.New("rollout item payload_json is invalid")
	}
	if value.CreatedAt.IsZero() {
		return errors.New("rollout item created_at is zero")
	}
	return nil
}

type TransitionError struct {
	Entity string
	ID     string
	From   string
	To     string
}

func (value *TransitionError) Error() string {
	return fmt.Sprintf("invalid %s transition for %q: %s -> %s", value.Entity, value.ID, value.From, value.To)
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
