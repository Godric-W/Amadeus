package local

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type durableRecorder interface {
	Append(context.Context, ...rollout.RolloutItem) ([]rollout.Line, error)
	Flush(context.Context) error
	Close(context.Context) error
	Path() string
}

func (store *Store) Materialize(ctx context.Context, input threadstore.CreateInput) (threadstore.AppendResult, error) {
	if input.CreatedAt.IsZero() {
		input.CreatedAt = store.clock().UTC()
	}
	if input.Source.Kind == "" {
		input.Source = protocol.RootSessionSource()
	}
	if err := input.Validate(); err != nil {
		return threadstore.AppendResult{}, err
	}
	path := store.rolloutPath(input.ID, input.CreatedAt)
	recorder, err := rollout.Create(path, input.ID, store.clock)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	store.mu.Lock()
	if _, exists := store.recorders[input.ID]; exists {
		store.mu.Unlock()
		_ = recorder.Close(context.Background())
		return threadstore.AppendResult{}, fmt.Errorf("thread %q already has an active writer", input.ID)
	}
	store.recorders[input.ID] = recorder
	store.mu.Unlock()
	meta := rollout.SessionMetaItem{
		SessionID: input.SessionID, ID: input.ID, Source: input.Source.Clone(), CWD: input.CWD, Title: input.Title, ModelProvider: input.ModelProvider, Model: input.Model, BaseInstructions: input.BaseInstructions.Clone(),
		GitSHA: input.GitSHA, GitBranch: input.GitBranch, GitOriginURL: input.GitOriginURL, CreatedAt: input.CreatedAt.UTC(),
	}
	if input.Source.IsSubAgent() {
		parent := input.Source.SubAgent.ParentThreadID
		meta.ParentThreadID = &parent
	}
	if err := meta.Validate(); err != nil {
		_ = store.CloseWriter(context.Background(), input.ID)
		return threadstore.AppendResult{}, err
	}
	lines, err := recorder.Append(ctx, meta)
	if err != nil {
		_ = store.CloseWriter(context.Background(), input.ID)
		return threadstore.AppendResult{}, err
	}
	if err := recorder.Flush(ctx); err != nil {
		_ = store.CloseWriter(context.Background(), input.ID)
		return threadstore.AppendResult{}, err
	}
	metadata, err := projectMetadata(path, lines)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	warning := store.state.UpsertThread(ctx, metadata)
	return appendResult(lines, warning), nil
}

func (store *Store) OpenWriter(ctx context.Context, id protocol.ThreadID) (threadstore.InitialHistory, error) {
	metadata, err := store.state.GetThread(ctx, id)
	if err != nil {
		return threadstore.InitialHistory{}, err
	}
	if err := validateRolloutPathIdentity(metadata.RolloutPath, id); err != nil {
		return threadstore.InitialHistory{}, err
	}
	store.mu.Lock()
	if _, exists := store.recorders[id]; exists {
		store.mu.Unlock()
		return threadstore.InitialHistory{}, fmt.Errorf("thread %q already has an active writer", id)
	}
	store.mu.Unlock()
	recorder, lines, err := rollout.Open(metadata.RolloutPath, id, store.clock)
	if err != nil {
		return threadstore.InitialHistory{}, err
	}
	store.mu.Lock()
	if _, exists := store.recorders[id]; exists {
		store.mu.Unlock()
		_ = recorder.Close(context.Background())
		return threadstore.InitialHistory{}, fmt.Errorf("thread %q already has an active writer", id)
	}
	store.recorders[id] = recorder
	store.mu.Unlock()
	return threadstore.InitialHistory{Kind: threadstore.InitialHistoryResumed, Lines: lines}, nil
}

func (store *Store) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, error) {
	result, recorder, err := store.appendItems(ctx, id, turnID, items...)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	if err := recorder.Flush(ctx); err != nil {
		return threadstore.AppendResult{}, err
	}
	result.MetadataWarning = errors.Join(store.syncMetadata(ctx, id, recorder), store.syncAgentEdges(ctx, items))
	return result, nil
}

func (store *Store) AppendItemsBuffered(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, error) {
	result, _, err := store.appendItems(ctx, id, turnID, items...)
	return result, err
}

func (store *Store) appendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, durableRecorder, error) {
	recorder, err := store.recorder(id)
	if err != nil {
		return threadstore.AppendResult{}, nil, err
	}
	scoped := make([]rollout.RolloutItem, len(items))
	for index, item := range items {
		scoped[index] = rollout.ScopeItem(item, id, turnID)
	}
	lines, err := recorder.Append(ctx, scoped...)
	if err != nil {
		return threadstore.AppendResult{}, nil, err
	}
	return appendResult(lines, nil), recorder, nil
}

func appendResult(lines []rollout.Line, warning error) threadstore.AppendResult {
	result := threadstore.AppendResult{Count: len(lines), MetadataWarning: warning}
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

func (store *Store) syncAgentEdges(ctx context.Context, items []rollout.RolloutItem) error {
	var result error
	for _, item := range items {
		edge, ok := item.(rollout.AgentSpawnEdgeItem)
		if !ok {
			continue
		}
		result = errors.Join(result, store.state.UpdateAgentEdgeState(ctx, edge.AgentID, edge.State, edge.UpdatedAt))
	}
	return result
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

func (store *Store) LoadHistory(ctx context.Context, id protocol.ThreadID) (threadstore.InitialHistory, error) {
	metadata, err := store.state.GetThread(ctx, id)
	if err != nil {
		return threadstore.InitialHistory{}, err
	}
	if err := ctx.Err(); err != nil {
		return threadstore.InitialHistory{}, err
	}
	lines, err := rollout.Read(metadata.RolloutPath, id)
	if err != nil {
		return threadstore.InitialHistory{}, err
	}
	return threadstore.InitialHistory{Kind: threadstore.InitialHistoryResumed, Lines: lines}, nil
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

func (store *Store) appendWithTemporaryWriter(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, item rollout.RolloutItem) (threadstore.AppendResult, error) {
	store.mu.Lock()
	_, active := store.recorders[id]
	store.mu.Unlock()
	if active {
		return store.AppendItems(ctx, id, turnID, item)
	}
	if _, err := store.OpenWriter(ctx, id); err != nil {
		return threadstore.AppendResult{}, err
	}
	defer store.CloseWriter(context.Background(), id)
	return store.AppendItems(ctx, id, turnID, item)
}
