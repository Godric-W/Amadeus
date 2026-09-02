package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (store *Store) GetThread(ctx context.Context, id protocol.ThreadID) (threadstore.StoredThread, error) {
	return store.state.GetThread(ctx, id)
}

func (store *Store) ListThreads(ctx context.Context, query threadstore.ListQuery) ([]threadstore.StoredThread, error) {
	return store.state.ListThreads(ctx, query)
}

func (store *Store) ListOpenChildren(ctx context.Context, parentID protocol.ThreadID) ([]threadstore.StoredThread, error) {
	return store.state.ListOpenChildren(ctx, parentID)
}

func (store *Store) RenameThread(ctx context.Context, id protocol.ThreadID, title string, at time.Time) error {
	if at.IsZero() {
		return errors.New("thread rename time is zero")
	}
	item := rollout.EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{Name: strings.TrimSpace(title)}}
	_, err := store.appendWithTemporaryWriter(ctx, id, "", item)
	return err
}

func (store *Store) DeleteThread(ctx context.Context, id protocol.ThreadID) error {
	if id.IsZero() {
		return errors.New("thread delete ID is empty")
	}
	store.mu.Lock()
	_, active := store.recorders[id]
	store.mu.Unlock()
	if active {
		return errors.New("cannot delete thread while its writer is active")
	}
	metadata, err := store.state.GetThread(ctx, id)
	if errors.Is(err, threadstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := store.validateDeletePath(metadata.RolloutPath, id); err != nil {
		return err
	}
	if err := os.Remove(metadata.RolloutPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete thread rollout: %w", err)
	}
	if err := store.state.DeleteThread(ctx, id); err != nil && !errors.Is(err, threadstore.ErrNotFound) {
		return fmt.Errorf("delete thread metadata: %w", err)
	}
	return nil
}

func (store *Store) validateDeletePath(path string, id protocol.ThreadID) error {
	if err := validateRolloutPathIdentity(path, id); err != nil {
		return err
	}
	sessionsRoot := filepath.Join(store.home, "sessions")
	relative, err := filepath.Rel(sessionsRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("thread rollout path is outside the sessions root")
	}
	return nil
}

func projectMetadata(path string, lines []rollout.Line) (threadstore.StoredThread, error) {
	if len(lines) == 0 {
		return threadstore.StoredThread{}, errors.New("rollout does not begin with session_meta")
	}
	meta, ok := lines[0].Item.(rollout.SessionMetaItem)
	if !ok {
		return threadstore.StoredThread{}, errors.New("rollout does not begin with session_meta")
	}
	if err := validateRolloutPathIdentity(path, meta.ID); err != nil {
		return threadstore.StoredThread{}, err
	}
	thread := threadstore.StoredThread{
		ID: meta.ID, Source: meta.Source.Clone(), RolloutPath: path, CWD: meta.CWD, Title: meta.Title,
		ModelProvider: meta.ModelProvider, Model: meta.Model, CreatedAt: meta.CreatedAt.UTC(), UpdatedAt: lines[0].Timestamp.UTC(),
		GitSHA: meta.GitSHA, GitBranch: meta.GitBranch, GitOriginURL: meta.GitOriginURL, Archived: meta.Archived,
	}
	for _, line := range lines[1:] {
		thread.UpdatedAt = line.Timestamp.UTC()
		switch item := line.Item.(type) {
		case rollout.EventMsgItem:
			switch event := item.Msg.(type) {
			case protocol.ThreadNameUpdatedEvent:
				if title := strings.TrimSpace(event.Name); title != "" {
					thread.Title = title
				}
			case protocol.ThreadArchivedEvent:
				thread.Archived = event.Archived
			case protocol.TokenCountEvent:
				if event.Info != nil {
					thread.TokensUsed = event.Info.TotalTokenUsage.TotalTokens
				}
			}
		case rollout.ResponseItem:
			if thread.Preview == "" {
				thread.Preview = responsePreview(item)
			}
		}
	}
	return thread, thread.Validate()
}

func validateRolloutPathIdentity(path string, id protocol.ThreadID) error {
	if id.IsZero() {
		return errors.New("rollout path thread ID is empty")
	}
	expectedSuffix := "-" + id.String() + ".jsonl"
	if !strings.HasSuffix(filepath.Base(path), expectedSuffix) {
		return fmt.Errorf("rollout filename does not match thread ID %q", id)
	}
	return nil
}

func responsePreview(item rollout.ResponseItem) string {
	if item.Type != rollout.ResponseUserMessage {
		return ""
	}
	content := strings.TrimSpace(item.Content)
	if len([]rune(content)) > 160 {
		content = string([]rune(content)[:160])
	}
	return content
}
