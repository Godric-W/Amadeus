package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

type scanner interface {
	Scan(...any) error
}

const sessionSelect = `SELECT id, project_id, title, status, next_run_sequence, next_item_sequence, created_at, updated_at, last_active_at FROM sessions`
const runSelect = `SELECT id, session_id, sequence, status, stop_reason, provider, model, api_mode, dialect, run_mode, usage_json, started_at, finished_at FROM runs`
const rolloutSelect = `SELECT id, session_id, run_id, sequence, kind, payload_json, created_at FROM rollout_items`

func scanProject(source scanner) (sessiondomain.Project, error) {
	var value sessiondomain.Project
	var createdAt, updatedAt, lastOpenedAt string
	if err := source.Scan(&value.ID, &value.CanonicalPath, &value.DisplayName, &createdAt, &updatedAt, &lastOpenedAt); err != nil {
		return sessiondomain.Project{}, sqliteStoreError("scan project", err)
	}
	var err error
	if value.CreatedAt, err = parseTime("project created_at", createdAt); err != nil {
		return sessiondomain.Project{}, err
	}
	if value.UpdatedAt, err = parseTime("project updated_at", updatedAt); err != nil {
		return sessiondomain.Project{}, err
	}
	if value.LastOpenedAt, err = parseTime("project last_opened_at", lastOpenedAt); err != nil {
		return sessiondomain.Project{}, err
	}
	return value, value.Validate()
}

func scanSession(source scanner) (sessiondomain.Session, error) {
	var value sessiondomain.Session
	var createdAt, updatedAt, lastActiveAt string
	if err := source.Scan(&value.ID, &value.ProjectID, &value.Title, &value.Status, &value.NextRunSequence, &value.NextItemSequence, &createdAt, &updatedAt, &lastActiveAt); err != nil {
		return sessiondomain.Session{}, sqliteStoreError("scan session", err)
	}
	var err error
	if value.CreatedAt, err = parseTime("session created_at", createdAt); err != nil {
		return sessiondomain.Session{}, err
	}
	if value.UpdatedAt, err = parseTime("session updated_at", updatedAt); err != nil {
		return sessiondomain.Session{}, err
	}
	if value.LastActiveAt, err = parseTime("session last_active_at", lastActiveAt); err != nil {
		return sessiondomain.Session{}, err
	}
	return value, value.Validate()
}

func scanRun(source scanner) (sessiondomain.Run, error) {
	var value sessiondomain.Run
	var stopReason, provider, model, apiMode, dialect, usageJSON, finishedAt sql.NullString
	var startedAt string
	if err := source.Scan(&value.ID, &value.SessionID, &value.Sequence, &value.Status, &stopReason, &provider, &model, &apiMode, &dialect, &value.Mode, &usageJSON, &startedAt, &finishedAt); err != nil {
		return sessiondomain.Run{}, sqliteStoreError("scan run", err)
	}
	value.StopReason = stopReason.String
	value.Provider = provider.String
	value.Model = model.String
	value.APIMode = apiMode.String
	value.Dialect = dialect.String
	if usageJSON.Valid {
		value.UsageJSON = json.RawMessage(usageJSON.String)
	}
	var err error
	if value.StartedAt, err = parseTime("run started_at", startedAt); err != nil {
		return sessiondomain.Run{}, err
	}
	if finishedAt.Valid {
		parsed, err := parseTime("run finished_at", finishedAt.String)
		if err != nil {
			return sessiondomain.Run{}, err
		}
		value.FinishedAt = &parsed
	}
	return value, value.Validate()
}

func scanRolloutItem(source scanner) (sessiondomain.RolloutItem, error) {
	var value sessiondomain.RolloutItem
	var runID sql.NullString
	var payload, createdAt string
	if err := source.Scan(&value.ID, &value.SessionID, &runID, &value.Sequence, &value.Kind, &payload, &createdAt); err != nil {
		return sessiondomain.RolloutItem{}, sqliteStoreError("scan rollout item", err)
	}
	if runID.Valid {
		value.RunID = sessiondomain.RunID(runID.String)
	}
	value.PayloadJSON = json.RawMessage(payload)
	var err error
	if value.CreatedAt, err = parseTime("rollout item created_at", createdAt); err != nil {
		return sessiondomain.RolloutItem{}, err
	}
	return value, value.Validate()
}

func parseTime(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse SQLite %s: %w", name, err)
	}
	return parsed.UTC(), nil
}
