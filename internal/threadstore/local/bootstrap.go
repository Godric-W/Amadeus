package local

import (
	"context"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func Open(ctx context.Context, home string, metadata threadstore.MetadataDB, clock rollout.Clock) (*Store, error) {
	if metadata == nil {
		return nil, errors.New("thread metadata store is nil")
	}
	store, err := NewStore(home, metadata, clock)
	if err != nil {
		return nil, err
	}
	if err := store.RebuildIndex(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("reconcile thread index: %w", err)
	}
	return store, nil
}
