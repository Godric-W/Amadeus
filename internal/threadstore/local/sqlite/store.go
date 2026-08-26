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

type Store struct {
	database *Database
}

func NewStore(database *Database) (*Store, error) {
	if database == nil || database.db == nil {
		return nil, errors.New("state database is nil")
	}
	return &Store{database: database}, nil
}

func (store *Store) UpsertThread(ctx context.Context, thread threadstore.StoredThread) error {
	if err := thread.Validate(); err != nil {
		return err
	}
	_, err := store.database.db.ExecContext(ctx, `INSERT INTO threads (
        id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role,
        rollout_path, cwd, title, preview, model_provider, model, tokens_used,
        created_at, updated_at, archived, git_sha, git_branch, git_origin_url
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
        source_kind = excluded.source_kind,
        parent_thread_id = excluded.parent_thread_id,
        agent_depth = excluded.agent_depth,
        agent_nickname = excluded.agent_nickname,
        agent_role = excluded.agent_role,
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
        git_origin_url = excluded.git_origin_url`,
		thread.ID.String(), string(thread.Source.Kind), sourceParentThreadID(thread.Source).String(), sourceDepth(thread.Source), sourceNickname(thread.Source), sourceRole(thread.Source),
		thread.RolloutPath, thread.CWD, thread.Title, thread.Preview,
		thread.ModelProvider, thread.Model, thread.TokensUsed,
		formatTime(thread.CreatedAt), formatTime(thread.UpdatedAt), thread.Archived,
		thread.GitSHA, thread.GitBranch, thread.GitOriginURL,
	)
	if err != nil {
		return fmt.Errorf("upsert thread metadata: %w", err)
	}
	return nil
}

func (store *Store) GetThread(ctx context.Context, id protocol.ThreadID) (threadstore.StoredThread, error) {
	row := store.database.db.QueryRowContext(ctx, `SELECT id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role,
        rollout_path, cwd, title, preview,
        model_provider, model, tokens_used, created_at, updated_at, archived,
	        git_sha, git_branch, git_origin_url FROM threads WHERE id = ?`, id.String())
	thread, err := scanThread(row)
	if errors.Is(err, sql.ErrNoRows) {
		return threadstore.StoredThread{}, threadstore.ErrNotFound
	}
	return thread, err
}

func (store *Store) ListThreads(ctx context.Context, query threadstore.ListQuery) ([]threadstore.StoredThread, error) {
	clauses := []string{"1 = 1"}
	arguments := make([]any, 0, 3)
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
	statement := `SELECT id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role,
        rollout_path, cwd, title, preview,
        model_provider, model, tokens_used, created_at, updated_at, archived,
        git_sha, git_branch, git_origin_url FROM threads WHERE ` + strings.Join(clauses, " AND ") + ` ORDER BY updated_at DESC, id`
	if query.Limit > 0 {
		statement += " LIMIT ?"
		arguments = append(arguments, query.Limit)
	}
	rows, err := store.database.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list thread metadata: %w", err)
	}
	defer rows.Close()
	threads := make([]threadstore.StoredThread, 0)
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread metadata: %w", err)
	}
	return threads, nil
}

func (store *Store) ListChildren(ctx context.Context, parentID protocol.ThreadID) ([]threadstore.StoredThread, error) {
	if parentID.IsZero() {
		return nil, errors.New("parent thread ID is empty")
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role,
		rollout_path, cwd, title, preview,
		model_provider, model, tokens_used, created_at, updated_at, archived,
		git_sha, git_branch, git_origin_url
		FROM threads WHERE source_kind = 'subagent' AND parent_thread_id = ? ORDER BY created_at, id`, parentID.String())
	if err != nil {
		return nil, fmt.Errorf("list child thread metadata: %w", err)
	}
	defer rows.Close()
	children := make([]threadstore.StoredThread, 0)
	for rows.Next() {
		child, scanErr := scanThread(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate child thread metadata: %w", err)
	}
	return children, nil
}

func (store *Store) RenameThread(ctx context.Context, id protocol.ThreadID, title string, updatedAt time.Time) error {
	title = strings.TrimSpace(title)
	if title == "" || updatedAt.IsZero() {
		return errors.New("thread rename is incomplete")
	}
	result, err := store.database.db.ExecContext(ctx, `UPDATE threads SET title = ?, updated_at = ? WHERE id = ?`, title, formatTime(updatedAt), id.String())
	if err != nil {
		return fmt.Errorf("rename thread metadata: %w", err)
	}
	return requireAffected(result)
}

func (store *Store) ArchiveThread(ctx context.Context, id protocol.ThreadID, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		return errors.New("thread archive time is zero")
	}
	result, err := store.database.db.ExecContext(ctx, `UPDATE threads SET archived = 1, updated_at = ? WHERE id = ?`, formatTime(updatedAt), id.String())
	if err != nil {
		return fmt.Errorf("archive thread metadata: %w", err)
	}
	return requireAffected(result)
}

func (store *Store) ReplaceThreads(ctx context.Context, threads []threadstore.StoredThread) error {
	tx, err := store.database.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin thread index rebuild: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM threads`); err != nil {
		return fmt.Errorf("clear thread index: %w", err)
	}
	for _, thread := range threads {
		if err := thread.Validate(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO threads (
            id, source_kind, parent_thread_id, agent_depth, agent_nickname, agent_role,
            rollout_path, cwd, title, preview, model_provider, model, tokens_used,
            created_at, updated_at, archived, git_sha, git_branch, git_origin_url
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			thread.ID.String(), string(thread.Source.Kind), sourceParentThreadID(thread.Source).String(), sourceDepth(thread.Source), sourceNickname(thread.Source), sourceRole(thread.Source),
			thread.RolloutPath, thread.CWD, thread.Title, thread.Preview,
			thread.ModelProvider, thread.Model, thread.TokensUsed,
			formatTime(thread.CreatedAt), formatTime(thread.UpdatedAt), thread.Archived,
			thread.GitSHA, thread.GitBranch, thread.GitOriginURL,
		); err != nil {
			return fmt.Errorf("rebuild thread index: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit thread index rebuild: %w", err)
	}
	return nil
}

func (store *Store) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}

type rowScanner interface {
	Scan(...any) error
}

func scanThread(scanner rowScanner) (threadstore.StoredThread, error) {
	var thread threadstore.StoredThread
	var threadID string
	var sourceKind string
	var parentThreadIDValue string
	var agentDepth int
	var agentNickname string
	var agentRole string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&threadID, &sourceKind, &parentThreadIDValue, &agentDepth, &agentNickname, &agentRole,
		&thread.RolloutPath, &thread.CWD, &thread.Title, &thread.Preview,
		&thread.ModelProvider, &thread.Model, &thread.TokensUsed, &createdAt, &updatedAt,
		&thread.Archived, &thread.GitSHA, &thread.GitBranch, &thread.GitOriginURL,
	); err != nil {
		return threadstore.StoredThread{}, err
	}
	parsedThreadID, err := protocol.ParseThreadID(threadID)
	if err != nil {
		return threadstore.StoredThread{}, fmt.Errorf("parse stored thread ID: %w", err)
	}
	thread.ID = parsedThreadID
	thread.Source = protocol.SessionSource{Kind: protocol.SessionSourceKind(sourceKind)}
	if thread.Source.Kind == protocol.SessionSourceSubAgent {
		parentThreadID, parseErr := protocol.ParseThreadID(parentThreadIDValue)
		if parseErr != nil {
			return threadstore.StoredThread{}, fmt.Errorf("parse stored parent thread ID: %w", parseErr)
		}
		thread.Source.SubAgent = &protocol.SubAgentSource{
			ParentThreadID: parentThreadID,
			Depth:          agentDepth,
			AgentNickname:  agentNickname,
			AgentRole:      agentRole,
		}
	}
	err = nil
	if thread.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return threadstore.StoredThread{}, fmt.Errorf("parse thread created_at: %w", err)
	}
	if thread.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return threadstore.StoredThread{}, fmt.Errorf("parse thread updated_at: %w", err)
	}
	return thread, thread.Validate()
}

func sourceParentThreadID(source protocol.SessionSource) protocol.ThreadID {
	if source.SubAgent == nil {
		return protocol.ThreadID{}
	}
	return source.SubAgent.ParentThreadID
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
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return threadstore.ErrNotFound
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
