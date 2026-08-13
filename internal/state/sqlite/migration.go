package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
)

type legacySession struct {
	ID        rollout.ThreadID
	CWD       string
	Title     string
	Archived  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type legacyRun struct {
	ID         rollout.TurnID
	Status     string
	StopReason string
	Provider   string
	Model      string
	StartedAt  time.Time
	FinishedAt *time.Time
}

type legacyItem struct {
	Kind      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

func migrateLegacyHistory(ctx context.Context, home string, database *sql.DB) error {
	exists, err := tableExists(ctx, database, "sessions")
	if err != nil || !exists {
		return err
	}
	statusExpression := `'active'`
	if hasStatus, columnErr := columnExists(ctx, database, "sessions", "status"); columnErr != nil {
		return columnErr
	} else if hasStatus {
		statusExpression = `COALESCE(s.status, 'active')`
	}
	rows, err := database.QueryContext(ctx, `SELECT s.id, p.canonical_path, s.title, `+statusExpression+`, s.created_at, s.updated_at
        FROM sessions s JOIN projects p ON p.id = s.project_id ORDER BY s.created_at, s.id`)
	if err != nil {
		return fmt.Errorf("query legacy sessions: %w", err)
	}
	sessions := make([]legacySession, 0)
	for rows.Next() {
		var value legacySession
		var createdAt string
		var updatedAt string
		var status string
		if err := rows.Scan(&value.ID, &value.CWD, &value.Title, &status, &createdAt, &updatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan legacy session: %w", err)
		}
		value.Archived = strings.EqualFold(strings.TrimSpace(status), "archived")
		if value.CreatedAt, err = parseLegacyTime(createdAt); err != nil {
			rows.Close()
			return err
		}
		if value.UpdatedAt, err = parseLegacyTime(updatedAt); err != nil {
			rows.Close()
			return err
		}
		sessions = append(sessions, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	store, err := NewStore(&Database{path: filepath.Join(home, "data", "amadeus.db"), db: database})
	if err != nil {
		return err
	}
	for _, legacy := range sessions {
		if existing, err := store.GetThread(ctx, legacy.ID); err == nil {
			if _, readErr := rollout.Read(existing.RolloutPath, legacy.ID); readErr != nil {
				return fmt.Errorf("validate migrated rollout for %q: %w", legacy.ID, readErr)
			}
			continue
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		path := legacyRolloutPath(home, legacy.ID, legacy.CreatedAt)
		var metadata state.StoredThread
		if _, statErr := os.Stat(path); statErr == nil {
			metadata, err = migratedMetadata(path, legacy)
		} else if errors.Is(statErr, os.ErrNotExist) {
			metadata, err = exportLegacySession(ctx, home, database, legacy)
		} else {
			err = statErr
		}
		if err != nil {
			return err
		}
		if err := store.UpsertThread(ctx, metadata); err != nil {
			return err
		}
	}
	return dropLegacyTables(ctx, database)
}

func exportLegacySession(ctx context.Context, home string, database *sql.DB, legacy legacySession) (state.StoredThread, error) {
	path := legacyRolloutPath(home, legacy.ID, legacy.CreatedAt)
	recorder, err := rollout.Create(path, legacy.ID, time.Now)
	if err != nil {
		return state.StoredThread{}, fmt.Errorf("create migrated rollout for %q: %w", legacy.ID, err)
	}
	closeWithError := func(exportErr error) (state.StoredThread, error) {
		closeErr := recorder.Close(context.Background())
		removeErr := os.Remove(path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return state.StoredThread{}, errors.Join(exportErr, closeErr, removeErr)
	}
	meta, err := rollout.NewItem(rollout.KindSessionMeta, rollout.SessionMeta{CWD: legacy.CWD, Title: legacy.Title, Archived: legacy.Archived, CreatedAt: legacy.CreatedAt})
	if err != nil {
		return closeWithError(err)
	}
	if _, err := recorder.Append(ctx, "", meta); err != nil {
		return closeWithError(err)
	}
	runs, err := loadLegacyRuns(ctx, database, legacy.ID)
	if err != nil {
		return closeWithError(err)
	}
	preview := ""
	provider := ""
	model := ""
	for _, run := range runs {
		if provider == "" {
			provider = run.Provider
			model = run.Model
		}
		items, err := loadLegacyItems(ctx, database, legacy.ID, run.ID)
		if err != nil {
			return closeWithError(err)
		}
		turnContext, err := rollout.NewRawItem(rollout.KindTurnContext, encodeRaw(map[string]any{
			"thread_id": legacy.ID, "turn_id": run.ID, "provider": fallback(run.Provider, "unknown"),
			"model": fallback(run.Model, "unknown"), "cwd": legacy.CWD, "initial_permission_mode": "default",
		}))
		if err != nil {
			return closeWithError(err)
		}
		canonical := []rollout.Item{turnContext}
		input := "continued legacy turn"
		for _, item := range items {
			if item.Kind != "user_message" {
				continue
			}
			var payload struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(item.Payload, &payload) == nil && strings.TrimSpace(payload.Content) != "" {
				input = strings.TrimSpace(payload.Content)
				if preview == "" {
					preview = truncateRunes(input, 160)
				}
				canonical = append(canonical, rawResponse("user_message", "user", payload.Content, "", "", item.Payload))
				break
			}
		}
		started, _ := rollout.NewItem(rollout.KindTurnStarted, rollout.TurnStarted{Input: input})
		canonical = append(canonical, started)
		for _, item := range items {
			converted, err := convertLegacyItem(item)
			if err != nil {
				return closeWithError(err)
			}
			canonical = append(canonical, converted...)
		}
		terminal, err := legacyTerminal(run)
		if err != nil {
			return closeWithError(err)
		}
		canonical = append(canonical, terminal)
		if _, err := recorder.Append(ctx, run.ID, canonical...); err != nil {
			return closeWithError(err)
		}
	}
	if err := recorder.Flush(ctx); err != nil {
		return closeWithError(err)
	}
	if err := recorder.Close(ctx); err != nil {
		return state.StoredThread{}, err
	}
	return state.StoredThread{
		ID: legacy.ID, RolloutPath: path, CWD: legacy.CWD, Title: legacy.Title, Preview: preview,
		ModelProvider: provider, Model: model, Archived: legacy.Archived, CreatedAt: legacy.CreatedAt, UpdatedAt: legacy.UpdatedAt,
	}, nil
}

func migratedMetadata(path string, legacy legacySession) (state.StoredThread, error) {
	lines, err := rollout.Read(path, legacy.ID)
	if err != nil {
		return state.StoredThread{}, fmt.Errorf("read migrated rollout for %q: %w", legacy.ID, err)
	}
	if len(lines) == 0 || lines[0].Item.Kind != rollout.KindSessionMeta {
		return state.StoredThread{}, fmt.Errorf("migrated rollout for %q has no session metadata", legacy.ID)
	}
	meta, err := rollout.DecodePayload[rollout.SessionMeta](lines[0].Item)
	if err != nil {
		return state.StoredThread{}, err
	}
	metadata := state.StoredThread{
		ID: legacy.ID, RolloutPath: path, CWD: meta.CWD, Title: meta.Title,
		ModelProvider: meta.ModelProvider, Model: meta.Model, Archived: meta.Archived, CreatedAt: legacy.CreatedAt, UpdatedAt: legacy.UpdatedAt,
		GitSHA: meta.GitSHA, GitBranch: meta.GitBranch, GitOriginURL: meta.GitOriginURL,
	}
	for _, line := range lines {
		if line.Item.Kind == rollout.KindTurnContext && metadata.ModelProvider == "" {
			var turnContext struct {
				Provider string `json:"provider"`
				Model    string `json:"model"`
			}
			if json.Unmarshal(line.Item.Payload, &turnContext) == nil {
				metadata.ModelProvider = strings.TrimSpace(turnContext.Provider)
				metadata.Model = strings.TrimSpace(turnContext.Model)
			}
		}
		if line.Item.Kind == rollout.KindResponseItem && metadata.Preview == "" {
			var response struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if json.Unmarshal(line.Item.Payload, &response) == nil && (response.Type == "user_message" || response.Role == "user") {
				metadata.Preview = truncateRunes(response.Content, 160)
			}
		}
	}
	return metadata, metadata.Validate()
}

func loadLegacyRuns(ctx context.Context, database *sql.DB, sessionID rollout.ThreadID) ([]legacyRun, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, status, COALESCE(stop_reason, ''), COALESCE(provider, ''),
        COALESCE(model, ''), started_at, finished_at FROM runs WHERE session_id = ? ORDER BY sequence, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]legacyRun, 0)
	for rows.Next() {
		var value legacyRun
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&value.ID, &value.Status, &value.StopReason, &value.Provider, &value.Model, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		var err error
		if value.StartedAt, err = parseLegacyTime(startedAt); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			parsed, err := parseLegacyTime(finishedAt.String)
			if err != nil {
				return nil, err
			}
			value.FinishedAt = &parsed
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func loadLegacyItems(ctx context.Context, database *sql.DB, sessionID rollout.ThreadID, runID rollout.TurnID) ([]legacyItem, error) {
	rows, err := database.QueryContext(ctx, `SELECT kind, payload_json, created_at FROM rollout_items
        WHERE session_id = ? AND run_id = ? ORDER BY sequence, id`, sessionID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]legacyItem, 0)
	for rows.Next() {
		var value legacyItem
		var payload string
		var createdAt string
		if err := rows.Scan(&value.Kind, &payload, &createdAt); err != nil {
			return nil, err
		}
		value.Payload = json.RawMessage(payload)
		parsed, err := parseLegacyTime(createdAt)
		if err != nil {
			return nil, err
		}
		value.CreatedAt = parsed
		values = append(values, value)
	}
	return values, rows.Err()
}

func convertLegacyItem(item legacyItem) ([]rollout.Item, error) {
	switch item.Kind {
	case "user_message":
		return nil, nil
	case "assistant_message":
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(item.Payload, &payload); err != nil {
			return nil, err
		}
		return []rollout.Item{rawResponse("assistant_message", "assistant", payload.Content, "", "", item.Payload)}, nil
	case "tool_call":
		var payload struct {
			Content string `json:"content"`
			Calls   []struct {
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"calls"`
		}
		if err := json.Unmarshal(item.Payload, &payload); err != nil {
			return nil, err
		}
		values := make([]rollout.Item, 0, len(payload.Calls)+1)
		if strings.TrimSpace(payload.Content) != "" {
			values = append(values, rawResponse("assistant_message", "assistant", payload.Content, "", "", item.Payload))
		}
		for _, call := range payload.Calls {
			value, err := rollout.NewRawItem(rollout.KindResponseItem, encodeRaw(map[string]any{
				"type": "tool_call", "call_id": call.ID, "name": call.Name, "arguments": call.Arguments,
			}))
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case "tool_result":
		var payload map[string]any
		if err := json.Unmarshal(item.Payload, &payload); err != nil {
			return nil, err
		}
		payload["type"] = "tool_result"
		if callID, ok := payload["call_id"]; !ok || strings.TrimSpace(fmt.Sprint(callID)) == "" {
			return nil, nil
		}
		value, err := rollout.NewRawItem(rollout.KindResponseItem, encodeRaw(payload))
		return []rollout.Item{value}, err
	case "plan_update":
		value, err := rollout.NewRawItem(rollout.KindPlanUpdate, item.Payload)
		return []rollout.Item{value}, err
	case "context_compaction":
		value, err := rollout.NewRawItem(rollout.KindCompaction, item.Payload)
		return []rollout.Item{value}, err
	case "run_interrupted", "run_failed":
		return nil, nil
	default:
		value, err := rollout.NewRawItem(rollout.Kind("legacy_"+item.Kind), item.Payload)
		return []rollout.Item{value}, err
	}
}

func legacyTerminal(run legacyRun) (rollout.Item, error) {
	switch run.Status {
	case "completed":
		return rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusCompleted})
	case "failed":
		return rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusFailed, Error: fallback(run.StopReason, "legacy turn failed")})
	default:
		return rollout.NewItem(rollout.KindTurnAborted, rollout.TurnAborted{Reason: fallback(run.StopReason, "legacy turn did not complete")})
	}
}

func dropLegacyTables(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"rollout_items", "runs", "sessions", "projects"} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return fmt.Errorf("drop legacy table %s: %w", table, err)
		}
	}
	return tx.Commit()
}

func tableExists(ctx context.Context, database *sql.DB, name string) (bool, error) {
	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count)
	return count > 0, err
}

func columnExists(ctx context.Context, database *sql.DB, table, column string) (bool, error) {
	rows, err := database.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var sequence int
		var name string
		var dataType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&sequence, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func parseLegacyTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse legacy timestamp %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

func legacyRolloutPath(home string, id rollout.ThreadID, at time.Time) string {
	stamp := at.UTC().Format("2006-01-02T15-04-05.000000000Z")
	return filepath.Join(home, "sessions", at.UTC().Format("2006"), at.UTC().Format("01"), at.UTC().Format("02"), fmt.Sprintf("rollout-%s-%s.jsonl", stamp, id))
}

func rawResponse(itemType, role, content, callID, name string, original json.RawMessage) rollout.Item {
	payload := map[string]any{"type": itemType, "role": role, "content": content}
	if callID != "" {
		payload["call_id"] = callID
	}
	if name != "" {
		payload["name"] = name
	}
	if len(original) > 0 {
		payload["legacy_payload"] = original
	}
	value, err := rollout.NewRawItem(rollout.KindResponseItem, encodeRaw(payload))
	if err != nil {
		panic(err)
	}
	return value
}

func encodeRaw(value any) json.RawMessage {
	content, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return content
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return strings.TrimSpace(value)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
