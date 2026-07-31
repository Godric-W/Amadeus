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

const sessionSelect = `SELECT id, project_id, title, status, next_turn_sequence, created_at, updated_at, last_active_at FROM conversation_sessions`

const runSelect = `SELECT id, session_id, turn_id, context_from_run_id, objective, status, stop_reason,
    provider, model, api_mode, dialect, budget_json, usage_json, latest_checkpoint_seq, started_at, finished_at FROM runs`

func scanProject(source scanner) (sessiondomain.Project, error) {
	var project sessiondomain.Project
	var createdAt, updatedAt, lastOpenedAt string
	if err := source.Scan(&project.ID, &project.CanonicalPath, &project.DisplayName, &createdAt, &updatedAt, &lastOpenedAt); err != nil {
		return sessiondomain.Project{}, sqliteStoreError("scan project", err)
	}
	var err error
	if project.CreatedAt, err = parseTime("project created_at", createdAt); err != nil {
		return sessiondomain.Project{}, err
	}
	if project.UpdatedAt, err = parseTime("project updated_at", updatedAt); err != nil {
		return sessiondomain.Project{}, err
	}
	if project.LastOpenedAt, err = parseTime("project last_opened_at", lastOpenedAt); err != nil {
		return sessiondomain.Project{}, err
	}
	return project, project.Validate()
}

func scanSession(source scanner) (sessiondomain.ConversationSession, error) {
	var conversation sessiondomain.ConversationSession
	var createdAt, updatedAt, lastActiveAt string
	if err := source.Scan(
		&conversation.ID, &conversation.ProjectID, &conversation.Title, &conversation.Status,
		&conversation.NextTurnSequence, &createdAt, &updatedAt, &lastActiveAt,
	); err != nil {
		return sessiondomain.ConversationSession{}, sqliteStoreError("scan conversation session", err)
	}
	var err error
	if conversation.CreatedAt, err = parseTime("conversation session created_at", createdAt); err != nil {
		return sessiondomain.ConversationSession{}, err
	}
	if conversation.UpdatedAt, err = parseTime("conversation session updated_at", updatedAt); err != nil {
		return sessiondomain.ConversationSession{}, err
	}
	if conversation.LastActiveAt, err = parseTime("conversation session last_active_at", lastActiveAt); err != nil {
		return sessiondomain.ConversationSession{}, err
	}
	return conversation, conversation.Validate()
}

func scanTurn(source scanner) (sessiondomain.Turn, error) {
	var turn sessiondomain.Turn
	var createdAt string
	var completedAt sql.NullString
	if err := source.Scan(&turn.ID, &turn.SessionID, &turn.Sequence, &turn.Status, &createdAt, &completedAt); err != nil {
		return sessiondomain.Turn{}, sqliteStoreError("scan turn", err)
	}
	var err error
	if turn.CreatedAt, err = parseTime("turn created_at", createdAt); err != nil {
		return sessiondomain.Turn{}, err
	}
	if completedAt.Valid {
		value, err := parseTime("turn completed_at", completedAt.String)
		if err != nil {
			return sessiondomain.Turn{}, err
		}
		turn.CompletedAt = &value
	}
	return turn, turn.Validate()
}

func scanMessage(source scanner) (sessiondomain.Message, error) {
	var message sessiondomain.Message
	var createdAt string
	if err := source.Scan(&message.ID, &message.SessionID, &message.TurnID, &message.Sequence, &message.Role, &message.Content, &createdAt); err != nil {
		return sessiondomain.Message{}, sqliteStoreError("scan conversation message", err)
	}
	var err error
	if message.CreatedAt, err = parseTime("message created_at", createdAt); err != nil {
		return sessiondomain.Message{}, err
	}
	return message, message.Validate()
}

func scanRun(source scanner) (sessiondomain.Run, error) {
	var run sessiondomain.Run
	var contextRun, stopReason, provider, model, apiMode, dialect, budgetJSON, usageJSON, finishedAt sql.NullString
	var startedAt string
	if err := source.Scan(
		&run.ID, &run.SessionID, &run.TurnID, &contextRun, &run.Objective, &run.Status, &stopReason,
		&provider, &model, &apiMode, &dialect, &budgetJSON, &usageJSON, &run.LatestCheckpointSequence, &startedAt, &finishedAt,
	); err != nil {
		return sessiondomain.Run{}, sqliteStoreError("scan run", err)
	}
	if contextRun.Valid {
		run.ContextFromRunID = sessiondomain.RunID(contextRun.String)
	}
	run.StopReason = stopReason.String
	run.Provider = provider.String
	run.Model = model.String
	run.APIMode = apiMode.String
	run.Dialect = dialect.String
	if budgetJSON.Valid {
		run.BudgetJSON = json.RawMessage(budgetJSON.String)
	}
	if usageJSON.Valid {
		run.UsageJSON = json.RawMessage(usageJSON.String)
	}
	var err error
	if run.StartedAt, err = parseTime("run started_at", startedAt); err != nil {
		return sessiondomain.Run{}, err
	}
	if finishedAt.Valid {
		value, err := parseTime("run finished_at", finishedAt.String)
		if err != nil {
			return sessiondomain.Run{}, err
		}
		run.FinishedAt = &value
	}
	return run, run.Validate()
}

func scanCheckpoint(source scanner) (sessiondomain.Checkpoint, error) {
	var checkpoint sessiondomain.Checkpoint
	var payloadJSON, createdAt string
	if err := source.Scan(
		&checkpoint.ID, &checkpoint.RunID, &checkpoint.Sequence, &checkpoint.SchemaVersion,
		&checkpoint.Reason, &payloadJSON, &checkpoint.PayloadHash, &createdAt,
	); err != nil {
		return sessiondomain.Checkpoint{}, sqliteStoreError("scan run checkpoint", err)
	}
	checkpoint.PayloadJSON = json.RawMessage(payloadJSON)
	var err error
	if checkpoint.CreatedAt, err = parseTime("checkpoint created_at", createdAt); err != nil {
		return sessiondomain.Checkpoint{}, err
	}
	return checkpoint, checkpoint.VerifyPayload()
}

func scanSummary(source scanner) (sessiondomain.ConversationSummary, error) {
	var summary sessiondomain.ConversationSummary
	var provider, model sql.NullString
	var createdAt string
	if err := source.Scan(
		&summary.ID, &summary.SessionID, &summary.FromMessageSequence, &summary.ToMessageSequence,
		&summary.Content, &summary.SourceHash, &summary.SummaryHash, &provider, &model, &createdAt,
	); err != nil {
		return sessiondomain.ConversationSummary{}, sqliteStoreError("scan conversation summary", err)
	}
	summary.Provider = provider.String
	summary.Model = model.String
	var err error
	if summary.CreatedAt, err = parseTime("conversation summary created_at", createdAt); err != nil {
		return sessiondomain.ConversationSummary{}, err
	}
	return summary, summary.Validate()
}

func parseTime(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse SQLite %s: %w", name, err)
	}
	return parsed.UTC(), nil
}
