package local

import (
	"context"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/rollout"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
)

func Open(ctx context.Context, home string, clock rollout.Clock) (*Store, error) {
	database, err := statesqlite.Open(ctx, home)
	if err != nil {
		return nil, err
	}
	stateStore, err := statesqlite.NewStore(database)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	store, err := NewStore(home, stateStore, clock)
	if err != nil {
		_ = stateStore.Close()
		return nil, err
	}
	if err := store.RebuildIndex(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("reconcile thread index: %w", err)
	}
	return store, nil
}
