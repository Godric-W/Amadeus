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
	state := &writerState{recorder: recorder}
	store.recorders[input.ID] = state
	store.mu.Unlock()
	cleanup := true
	defer func() {
		if cleanup {
			_ = store.DiscardWriter(context.Background(), input.ID)
		}
	}()
	meta := rollout.SessionMetaItem{
		SessionID: input.SessionID, ID: input.ID, Source: input.Source.Clone(), CWD: input.CWD, Title: input.Title, ModelProvider: input.ModelProvider, Model: input.Model, BaseInstructions: input.BaseInstructions.Clone(),
		GitSHA: input.GitSHA, GitBranch: input.GitBranch, GitOriginURL: input.GitOriginURL, CreatedAt: input.CreatedAt.UTC(),
	}
	if input.Source.IsSubAgent() {
		parent := input.Source.SubAgent.ParentThreadID
		meta.ParentThreadID = &parent
	}
	if err := meta.Validate(); err != nil {
		return threadstore.AppendResult{}, err
	}
	lines, err := recorder.Append(ctx, meta)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	if err := recorder.Flush(ctx); err != nil {
		return threadstore.AppendResult{}, err
	}
	metadata, err := projectMetadata(path, lines)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	warning := store.state.UpsertThread(ctx, metadata)
	state.mu.Lock()
	state.metadata = newMetadataSync(metadata)
	state.mu.Unlock()
	cleanup = false
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
	metadata, projectErr := projectMetadata(metadata.RolloutPath, lines)
	if projectErr != nil {
		store.mu.Unlock()
		_ = recorder.Close(context.Background())
		return threadstore.InitialHistory{}, projectErr
	}
	store.recorders[id] = &writerState{recorder: recorder, metadata: newMetadataSync(metadata)}
	store.mu.Unlock()
	return threadstore.InitialHistory{Kind: threadstore.InitialHistoryResumed, Lines: lines}, nil
}

func (store *Store) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, error) {
	result, state, lines, err := store.appendItems(ctx, id, turnID, items...)
	if err != nil {
		return threadstore.AppendResult{}, err
	}
	state.mu.Lock()
	metadataErr := state.metadata.observe(lines)
	state.mu.Unlock()
	if metadataErr != nil {
		return threadstore.AppendResult{}, metadataErr
	}
	if err := state.recorder.Flush(ctx); err != nil {
		return threadstore.AppendResult{}, err
	}
	metadataErr = store.syncMetadataState(ctx, state)
	result.MetadataWarning = errors.Join(metadataErr, store.syncAgentEdges(ctx, items))
	return result, nil
}

func (store *Store) AppendItemsBuffered(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, error) {
	result, state, lines, err := store.appendItems(ctx, id, turnID, items...)
	if err != nil {
		return result, err
	}
	state.mu.Lock()
	err = state.metadata.observe(lines)
	state.mu.Unlock()
	return result, err
}

func (store *Store) appendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (threadstore.AppendResult, *writerState, []rollout.Line, error) {
	state, err := store.recorder(id)
	if err != nil {
		return threadstore.AppendResult{}, nil, nil, err
	}
	scoped := make([]rollout.RolloutItem, len(items))
	for index, item := range items {
		scoped[index] = rollout.ScopeItem(item, id, turnID)
	}
	lines, err := state.recorder.Append(ctx, scoped...)
	if err != nil {
		return threadstore.AppendResult{}, nil, nil, err
	}
	return appendResult(lines, nil), state, lines, nil
}

func appendResult(lines []rollout.Line, warning error) threadstore.AppendResult {
	result := threadstore.AppendResult{Count: len(lines), MetadataWarning: warning}
	if len(lines) > 0 {
		result.FirstSequence = lines[0].Sequence
	}
	return result
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
	state, err := store.recorder(id)
	if err != nil {
		return err
	}
	return store.flushState(ctx, state)
}

// flushState establishes the durability watermark first, then applies the
// metadata snapshot derived from all typed lines observed by this writer. A
// failed SQLite update leaves state.metadata.pending intact for a later retry.
func (store *Store) flushState(ctx context.Context, state *writerState) error {
	if state == nil || state.recorder == nil {
		return errors.New("writer state is nil")
	}
	if err := state.recorder.Flush(ctx); err != nil {
		return err
	}
	return store.syncMetadataState(ctx, state)
}

func (store *Store) syncMetadataState(ctx context.Context, state *writerState) error {
	if state == nil || state.recorder == nil {
		return errors.New("writer state is nil")
	}
	state.mu.Lock()
	metadata := state.metadata
	var snapshot *threadstore.StoredThread
	if metadata != nil {
		snapshot = metadata.durableSnapshot()
	}
	state.mu.Unlock()
	if snapshot == nil {
		return nil
	}
	if err := store.state.UpsertThread(ctx, *snapshot); err != nil {
		return err
	}
	state.mu.Lock()
	metadata.markPersisted()
	state.mu.Unlock()
	return nil
}

func (store *Store) CloseWriter(ctx context.Context, id protocol.ThreadID) error {
	store.mu.Lock()
	state, exists := store.recorders[id]
	if exists {
		delete(store.recorders, id)
	}
	store.mu.Unlock()
	if !exists {
		return nil
	}
	if err := state.recorder.Close(ctx); err != nil {
		return err
	}
	// Close flushes the canonical JSONL first. Only after that durable
	// boundary may buffered facts advance the SQLite read model.
	return store.syncMetadataState(ctx, state)
}

func (store *Store) DiscardWriter(ctx context.Context, id protocol.ThreadID) error {
	store.mu.Lock()
	state, exists := store.recorders[id]
	if exists {
		delete(store.recorders, id)
	}
	store.mu.Unlock()
	if !exists {
		return nil
	}
	discarder, ok := state.recorder.(interface{ Discard(context.Context) error })
	if !ok {
		return state.recorder.Close(ctx)
	}
	return discarder.Discard(ctx)
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

func (store *Store) recorder(id protocol.ThreadID) (*writerState, error) {
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
