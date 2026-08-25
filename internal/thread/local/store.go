package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
)

type Store struct {
	mu        sync.Mutex
	home      string
	state     state.DB
	clock     rollout.Clock
	recorders map[protocol.ThreadID]durableRecorder
}

type durableRecorder interface {
	Append(context.Context, ...rollout.RolloutItem) ([]rollout.Line, error)
	Flush(context.Context) error
	Close(context.Context) error
	Path() string
}

func NewStore(home string, stateDB state.DB, clock rollout.Clock) (*Store, error) {
	if strings.TrimSpace(home) == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return nil, errors.New("local thread store home must be a clean absolute path")
	}
	if stateDB == nil {
		return nil, errors.New("local thread store state DB is nil")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Store{home: home, state: stateDB, clock: clock, recorders: make(map[protocol.ThreadID]durableRecorder)}, nil
}

func (store *Store) Materialize(ctx context.Context, input thread.CreateInput) (thread.AppendResult, error) {
	if input.CreatedAt.IsZero() {
		input.CreatedAt = store.clock().UTC()
	}
	if input.Source.Kind == "" {
		input.Source = protocol.RootSessionSource()
	}
	if err := input.Validate(); err != nil {
		return thread.AppendResult{}, err
	}
	path := store.rolloutPath(input.ID, input.CreatedAt)
	recorder, err := rollout.Create(path, input.ID, store.clock)
	if err != nil {
		return thread.AppendResult{}, err
	}
	store.mu.Lock()
	if _, exists := store.recorders[input.ID]; exists {
		store.mu.Unlock()
		_ = recorder.Close(context.Background())
		return thread.AppendResult{}, fmt.Errorf("thread %q already has an active writer", input.ID)
	}
	store.recorders[input.ID] = recorder
	store.mu.Unlock()
	meta := rollout.SessionMetaItem{
		SessionID: input.SessionID, ID: input.ID, Source: input.Source.Clone(), CWD: input.CWD, Title: input.Title, ModelProvider: input.ModelProvider, Model: input.Model,
		GitSHA: input.GitSHA, GitBranch: input.GitBranch, GitOriginURL: input.GitOriginURL, CreatedAt: input.CreatedAt.UTC(),
	}
	if input.Source.IsSubAgent() {
		parent := input.Source.SubAgent.ParentThreadID
		meta.ParentThreadID = &parent
	}
	if err := meta.Validate(); err != nil {
		_ = store.CloseWriter(context.Background(), input.ID)
		return thread.AppendResult{}, err
	}
	lines, err := recorder.Append(ctx, meta)
	if err != nil {
		_ = store.CloseWriter(context.Background(), input.ID)
		return thread.AppendResult{}, err
	}
	if err := recorder.Flush(ctx); err != nil {
		_ = store.CloseWriter(context.Background(), input.ID)
		return thread.AppendResult{}, err
	}
	metadata, err := projectMetadata(path, lines)
	if err != nil {
		return thread.AppendResult{}, err
	}
	warning := store.state.UpsertThread(ctx, metadata)
	return appendResult(lines, warning), nil
}

func (store *Store) OpenWriter(ctx context.Context, id protocol.ThreadID) (thread.InitialHistory, error) {
	metadata, err := store.state.GetThread(ctx, id)
	if err != nil {
		return thread.InitialHistory{}, err
	}
	if err := validateRolloutPathIdentity(metadata.RolloutPath, id); err != nil {
		return thread.InitialHistory{}, err
	}
	store.mu.Lock()
	if _, exists := store.recorders[id]; exists {
		store.mu.Unlock()
		return thread.InitialHistory{}, fmt.Errorf("thread %q already has an active writer", id)
	}
	store.mu.Unlock()
	recorder, lines, err := rollout.Open(metadata.RolloutPath, id, store.clock)
	if err != nil {
		return thread.InitialHistory{}, err
	}
	store.mu.Lock()
	if _, exists := store.recorders[id]; exists {
		store.mu.Unlock()
		_ = recorder.Close(context.Background())
		return thread.InitialHistory{}, fmt.Errorf("thread %q already has an active writer", id)
	}
	store.recorders[id] = recorder
	store.mu.Unlock()
	return thread.InitialHistory{Kind: thread.InitialHistoryResumed, Lines: lines}, nil
}

func (store *Store) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (thread.AppendResult, error) {
	result, recorder, err := store.appendItems(ctx, id, turnID, items...)
	if err != nil {
		return thread.AppendResult{}, err
	}
	if err := recorder.Flush(ctx); err != nil {
		return thread.AppendResult{}, err
	}
	result.MetadataWarning = store.syncMetadata(ctx, id, recorder)
	return result, nil
}

func (store *Store) AppendItemsBuffered(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (thread.AppendResult, error) {
	result, _, err := store.appendItems(ctx, id, turnID, items...)
	return result, err
}

func (store *Store) appendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (thread.AppendResult, durableRecorder, error) {
	recorder, err := store.recorder(id)
	if err != nil {
		return thread.AppendResult{}, nil, err
	}
	scoped := make([]rollout.RolloutItem, len(items))
	for index, item := range items {
		scoped[index] = rollout.ScopeItem(item, id, turnID)
	}
	lines, err := recorder.Append(ctx, scoped...)
	if err != nil {
		return thread.AppendResult{}, nil, err
	}
	return appendResult(lines, nil), recorder, nil
}

func appendResult(lines []rollout.Line, warning error) thread.AppendResult {
	result := thread.AppendResult{Count: len(lines), MetadataWarning: warning}
	if len(lines) > 0 {
		result.FirstSequence = lines[0].Sequence
	}
	return result
}

func (store *Store) syncMetadata(ctx context.Context, id protocol.ThreadID, recorder durableRecorder) error {
	history, err := rollout.Read(recorder.Path(), id)
	if err != nil {
		return err
	}
	projected, err := projectMetadata(recorder.Path(), history)
	if err != nil {
		return err
	}
	return store.state.UpsertThread(ctx, projected)
}

func (store *Store) Flush(ctx context.Context, id protocol.ThreadID) error {
	recorder, err := store.recorder(id)
	if err != nil {
		return err
	}
	return recorder.Flush(ctx)
}

func (store *Store) CloseWriter(ctx context.Context, id protocol.ThreadID) error {
	store.mu.Lock()
	recorder, exists := store.recorders[id]
	if exists {
		delete(store.recorders, id)
	}
	store.mu.Unlock()
	if !exists {
		return nil
	}
	return recorder.Close(ctx)
}

func (store *Store) LoadHistory(ctx context.Context, id protocol.ThreadID) (thread.InitialHistory, error) {
	metadata, err := store.state.GetThread(ctx, id)
	if err != nil {
		return thread.InitialHistory{}, err
	}
	if err := ctx.Err(); err != nil {
		return thread.InitialHistory{}, err
	}
	lines, err := rollout.Read(metadata.RolloutPath, id)
	if err != nil {
		return thread.InitialHistory{}, err
	}
	return thread.InitialHistory{Kind: thread.InitialHistoryResumed, Lines: lines}, nil
}

func (store *Store) GetThread(ctx context.Context, id protocol.ThreadID) (state.StoredThread, error) {
	return store.state.GetThread(ctx, id)
}

func (store *Store) ListThreads(ctx context.Context, query state.ListQuery) ([]state.StoredThread, error) {
	return store.state.ListThreads(ctx, query)
}

func (store *Store) ListChildren(ctx context.Context, parentID protocol.ThreadID) ([]state.StoredThread, error) {
	return store.state.ListChildren(ctx, parentID)
}

func (store *Store) RenameThread(ctx context.Context, id protocol.ThreadID, title string, at time.Time) error {
	if at.IsZero() {
		return errors.New("thread rename time is zero")
	}
	item := rollout.EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{Name: strings.TrimSpace(title)}}
	_, err := store.appendWithTemporaryWriter(ctx, id, "", item)
	return err
}

func (store *Store) DeleteThread(ctx context.Context, id protocol.ThreadID, at time.Time) error {
	if at.IsZero() {
		return errors.New("thread archive time is zero")
	}
	item := rollout.EventMsgItem{Msg: protocol.ThreadArchivedEvent{Archived: true}}
	_, err := store.appendWithTemporaryWriter(ctx, id, "", item)
	return err
}

func (store *Store) RebuildIndex(ctx context.Context) error {
	root := filepath.Join(store.home, "sessions")
	threads := make([]state.StoredThread, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		lines, err := rollout.Read(path, protocol.ThreadID{})
		if err != nil {
			return err
		}
		metadata, err := projectMetadata(path, lines)
		if err != nil {
			return err
		}
		threads = append(threads, metadata)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return store.state.ReplaceThreads(ctx, nil)
	}
	if err != nil {
		return fmt.Errorf("scan rollouts for index rebuild: %w", err)
	}
	return store.state.ReplaceThreads(ctx, threads)
}

func (store *Store) Close() error {
	store.mu.Lock()
	recorders := make([]durableRecorder, 0, len(store.recorders))
	for id, recorder := range store.recorders {
		recorders = append(recorders, recorder)
		delete(store.recorders, id)
	}
	store.mu.Unlock()
	var result error
	for _, recorder := range recorders {
		result = errors.Join(result, recorder.Close(context.Background()))
	}
	return errors.Join(result, store.state.Close())
}

func (store *Store) recorder(id protocol.ThreadID) (durableRecorder, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	recorder, exists := store.recorders[id]
	if !exists {
		return nil, fmt.Errorf("thread %q has no active writer", id)
	}
	return recorder, nil
}

func (store *Store) appendWithTemporaryWriter(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, item rollout.RolloutItem) (thread.AppendResult, error) {
	store.mu.Lock()
	_, active := store.recorders[id]
	store.mu.Unlock()
	if active {
		return store.AppendItems(ctx, id, turnID, item)
	}
	if _, err := store.OpenWriter(ctx, id); err != nil {
		return thread.AppendResult{}, err
	}
	defer store.CloseWriter(context.Background(), id)
	return store.AppendItems(ctx, id, turnID, item)
}

func (store *Store) rolloutPath(id protocol.ThreadID, at time.Time) string {
	stamp := at.UTC().Format("2006-01-02T15-04-05.000000000Z")
	return filepath.Join(store.home, "sessions", at.UTC().Format("2006"), at.UTC().Format("01"), at.UTC().Format("02"), fmt.Sprintf("rollout-%s-%s.jsonl", stamp, id.String()))
}

func projectMetadata(path string, lines []rollout.Line) (state.StoredThread, error) {
	if len(lines) == 0 {
		return state.StoredThread{}, errors.New("rollout does not begin with session_meta")
	}
	meta, ok := lines[0].Item.(rollout.SessionMetaItem)
	if !ok {
		return state.StoredThread{}, errors.New("rollout does not begin with session_meta")
	}
	if err := validateRolloutPathIdentity(path, meta.ID); err != nil {
		return state.StoredThread{}, err
	}
	thread := state.StoredThread{
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
