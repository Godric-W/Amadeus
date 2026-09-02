package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type threadStore struct{ db *sql.DB }

func (store *threadStore) UpsertThread(ctx context.Context, thread threadstore.StoredThread) error {
	if err := thread.Validate(); err != nil {
		return err
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO threads (
		id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role, agent_edge_state,
		rollout_path, cwd, title, preview, model_provider, model, tokens_used,
		created_at, updated_at, archived, git_sha, git_branch, git_origin_url
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		source_kind = excluded.source_kind,
		parent_thread_id = excluded.parent_thread_id,
		agent_depth = excluded.agent_depth,
		agent_nickname = excluded.agent_nickname,
		agent_role = excluded.agent_role,
		agent_edge_state = CASE WHEN excluded.agent_edge_state = '' THEN threads.agent_edge_state ELSE excluded.agent_edge_state END,
		rollout_path = excluded.rollout_path,
		cwd = excluded.cwd,
		title = excluded.title,
		preview = excluded.preview,
		model_provider = excluded.model_provider,
		model = excluded.model,
		tokens_used = excluded.tokens_used,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at,
		archived = excluded.archived,
		git_sha = excluded.git_sha,
		git_branch = excluded.git_branch,
		git_origin_url = excluded.git_origin_url`, threadValues(thread)...)
	if err != nil {
		return fmt.Errorf("upsert thread metadata: %w", err)
	}
	return nil
}

func (store *threadStore) GetThread(ctx context.Context, id protocol.ThreadID) (threadstore.StoredThread, error) {
	thread, err := scanThread(store.db.QueryRowContext(ctx, threadSelect+` WHERE id = ?`, id.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return threadstore.StoredThread{}, threadstore.ErrNotFound
	}
	return thread, err
}

func (store *threadStore) ListThreads(ctx context.Context, query threadstore.ListQuery) ([]threadstore.StoredThread, error) {
	clauses := []string{"1 = 1"}
	arguments := make([]any, 0, 4)
	if cwd := strings.TrimSpace(query.CWD); cwd != "" {
		clauses = append(clauses, "cwd = ?")
		arguments = append(arguments, cwd)
	}
	if !query.IncludeArchived {
		clauses = append(clauses, "archived = 0")
	}
	if !query.IncludeSubAgents {
		clauses = append(clauses, "source_kind = 'root'")
	}
	statement := threadSelect + ` WHERE ` + strings.Join(clauses, " AND ") + ` ORDER BY updated_at DESC, id`
	if query.Limit > 0 {
		statement += " LIMIT ?"
		arguments = append(arguments, query.Limit)
	}
	rows, err := store.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list thread metadata: %w", err)
	}
	defer rows.Close()
	var threads []threadstore.StoredThread
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func (store *threadStore) ListOpenChildren(ctx context.Context, parentID protocol.ThreadID) ([]threadstore.StoredThread, error) {
	if parentID.IsZero() {
		return nil, errors.New("parent thread ID is empty")
	}
	rows, err := store.db.QueryContext(ctx, threadSelect+` WHERE source_kind = 'subagent' AND parent_thread_id = ? AND agent_edge_state = 'open' AND archived = 0 ORDER BY created_at, id`, parentID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var children []threadstore.StoredThread
	for rows.Next() {
		child, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, rows.Err()
}

func (store *threadStore) RenameThread(ctx context.Context, id protocol.ThreadID, title string, updatedAt time.Time) error {
	title = strings.TrimSpace(title)
	if title == "" || updatedAt.IsZero() {
		return errors.New("thread rename is incomplete")
	}
	result, err := store.db.ExecContext(ctx, `UPDATE threads SET title = ?, updated_at = ? WHERE id = ?`, title, formatTime(updatedAt), id.String())
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (store *threadStore) ArchiveThread(ctx context.Context, id protocol.ThreadID, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		return errors.New("thread archive time is zero")
	}
	result, err := store.db.ExecContext(ctx, `UPDATE threads SET archived = 1, updated_at = ? WHERE id = ?`, formatTime(updatedAt), id.String())
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (store *threadStore) DeleteThread(ctx context.Context, id protocol.ThreadID) error {
	if id.IsZero() {
		return errors.New("thread delete ID is empty")
	}
	result, err := store.db.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, id.String())
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (store *threadStore) SetPreviewIfEmpty(ctx context.Context, id protocol.ThreadID, preview string, updatedAt time.Time) error {
	preview = strings.TrimSpace(preview)
	if id.IsZero() || preview == "" || updatedAt.IsZero() {
		return errors.New("thread preview update is incomplete")
	}
	_, err := store.db.ExecContext(ctx, `UPDATE threads SET preview = CASE WHEN trim(preview) = '' THEN ? ELSE preview END, updated_at = CASE WHEN trim(preview) = '' THEN ? ELSE updated_at END WHERE id = ?`, preview, formatTime(updatedAt), id.String())
	return err
}

func (store *threadStore) UpdateAgentEdgeState(ctx context.Context, id protocol.ThreadID, edge protocol.AgentSpawnEdgeState, updatedAt time.Time) error {
	if id.IsZero() || !edge.Valid() || updatedAt.IsZero() {
		return errors.New("agent edge state update is incomplete")
	}
	result, err := store.db.ExecContext(ctx, `UPDATE threads SET agent_edge_state = ?, updated_at = ? WHERE id = ? AND source_kind = 'subagent'`, string(edge), formatTime(updatedAt), id.String())
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (store *threadStore) ReplaceThreads(ctx context.Context, threads []threadstore.StoredThread) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM threads`); err != nil {
		return err
	}
	for _, thread := range threads {
		if err := thread.Validate(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO threads (
			id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role, agent_edge_state,
			rollout_path, cwd, title, preview, model_provider, model, tokens_used,
			created_at, updated_at, archived, git_sha, git_branch, git_origin_url
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, threadValues(thread)...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const threadSelect = `SELECT id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role, agent_edge_state,
	rollout_path, cwd, title, preview, model_provider, model, tokens_used, created_at, updated_at, archived,
	git_sha, git_branch, git_origin_url FROM threads`

type rowScanner interface{ Scan(...any) error }

func scanThread(scanner rowScanner) (threadstore.StoredThread, error) {
	var thread threadstore.StoredThread
	var threadID, sourceKind, parentID, nickname, role, edge, createdAt, updatedAt string
	var depth int
	if err := scanner.Scan(&threadID, &sourceKind, &parentID, &depth, &nickname, &role, &edge,
		&thread.RolloutPath, &thread.CWD, &thread.Title, &thread.Preview, &thread.ModelProvider, &thread.Model,
		&thread.TokensUsed, &createdAt, &updatedAt, &thread.Archived, &thread.GitSHA, &thread.GitBranch, &thread.GitOriginURL); err != nil {
		return threadstore.StoredThread{}, err
	}
	id, err := protocol.ParseThreadID(threadID)
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	thread.ID = id
	thread.Source = protocol.SessionSource{Kind: protocol.SessionSourceKind(sourceKind)}
	thread.AgentEdgeState = protocol.AgentSpawnEdgeState(edge)
	if thread.Source.Kind == protocol.SessionSourceSubAgent {
		parent, err := protocol.ParseThreadID(parentID)
		if err != nil {
			return threadstore.StoredThread{}, err
		}
		thread.Source.SubAgent = &protocol.SubAgentSource{ParentThreadID: parent, Depth: depth, AgentNickname: nickname, AgentRole: role}
	}
	if thread.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return threadstore.StoredThread{}, err
	}
	if thread.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return threadstore.StoredThread{}, err
	}
	return thread, thread.Validate()
}

func threadValues(thread threadstore.StoredThread) []any {
	return []any{
		thread.ID.String(), string(thread.Source.Kind), sourceParentID(thread.Source), sourceDepth(thread.Source), sourceNickname(thread.Source), sourceRole(thread.Source), string(thread.AgentEdgeState),
		thread.RolloutPath, thread.CWD, thread.Title, thread.Preview, thread.ModelProvider, thread.Model, thread.TokensUsed,
		formatTime(thread.CreatedAt), formatTime(thread.UpdatedAt), thread.Archived, thread.GitSHA, thread.GitBranch, thread.GitOriginURL,
	}
}

func sourceParentID(source protocol.SessionSource) string {
	if source.SubAgent == nil {
		return ""
	}
	return source.SubAgent.ParentThreadID.String()
}

func sourceDepth(source protocol.SessionSource) int {
	if source.SubAgent == nil {
		return 0
	}
	return source.SubAgent.Depth
}

func sourceNickname(source protocol.SessionSource) string {
	if source.SubAgent == nil {
		return ""
	}
	return source.SubAgent.AgentNickname
}

func sourceRole(source protocol.SessionSource) string {
	if source.SubAgent == nil {
		return ""
	}
	return source.SubAgent.AgentRole
}

func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return threadstore.ErrNotFound
	}
	return nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

var _ threadstore.MetadataDB = (*threadStore)(nil)
