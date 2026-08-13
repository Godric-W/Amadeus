package thread

import (
	"context"
	"errors"
	"sync"

	"github.com/Godric-W/Amadeus/internal/rollout"
)

type LiveThread struct {
	mu           sync.Mutex
	id           ID
	store        ThreadStore
	materialized bool
	closed       bool
}

func NewDraftLiveThread(id ID, store ThreadStore) (*LiveThread, error) {
	if id == "" || store == nil {
		return nil, errors.New("draft live thread is incomplete")
	}
	return &LiveThread{id: id, store: store}, nil
}

func NewResumedLiveThread(ctx context.Context, id ID, store ThreadStore) (*LiveThread, InitialHistory, error) {
	if id == "" || store == nil {
		return nil, InitialHistory{}, errors.New("resumed live thread is incomplete")
	}
	history, err := store.OpenWriter(ctx, id)
	if err != nil {
		return nil, InitialHistory{}, err
	}
	return &LiveThread{id: id, store: store, materialized: true}, history, nil
}

func (thread *LiveThread) ID() ID {
	if thread == nil {
		return ""
	}
	return thread.id
}

func (thread *LiveThread) Materialize(ctx context.Context, input CreateInput) (AppendResult, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.closed {
		return AppendResult{}, errors.New("live thread is closed")
	}
	if thread.materialized {
		return AppendResult{}, nil
	}
	input.ID = thread.id
	result, err := thread.store.Materialize(ctx, input)
	if err != nil {
		return AppendResult{}, err
	}
	thread.materialized = true
	return result, nil
}

func (thread *LiveThread) AppendItems(ctx context.Context, turnID TurnID, items ...rollout.Item) (AppendResult, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.closed {
		return AppendResult{}, errors.New("live thread is closed")
	}
	if !thread.materialized {
		return AppendResult{}, errors.New("live thread is not materialized")
	}
	return thread.store.AppendItems(ctx, thread.id, turnID, items...)
}

// AppendItemsBuffered appends facts and updates the in-memory/index view but
// lets the store defer its fsync. Session uses this only for high-frequency
// intermediate response facts; terminal persistence still calls Flush before
// publishing the terminal SessionEvent.
func (thread *LiveThread) AppendItemsBuffered(ctx context.Context, turnID TurnID, items ...rollout.Item) (AppendResult, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.closed {
		return AppendResult{}, errors.New("live thread is closed")
	}
	if !thread.materialized {
		return AppendResult{}, errors.New("live thread is not materialized")
	}
	if buffered, ok := thread.store.(interface {
		AppendItemsBuffered(context.Context, ID, TurnID, ...rollout.Item) (AppendResult, error)
	}); ok {
		return buffered.AppendItemsBuffered(ctx, thread.id, turnID, items...)
	}
	return thread.store.AppendItems(ctx, thread.id, turnID, items...)
}

func (thread *LiveThread) Flush(ctx context.Context) error {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.closed || !thread.materialized {
		return nil
	}
	return thread.store.Flush(ctx, thread.id)
}

func (thread *LiveThread) Shutdown(ctx context.Context) error {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.closed {
		return nil
	}
	thread.closed = true
	if !thread.materialized {
		return nil
	}
	return thread.store.CloseWriter(ctx, thread.id)
}
