package bootstrap

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
	threadlocal "github.com/Godric-W/Amadeus/internal/threadstore/local"
)

type ThreadStoreFactory func(context.Context, string, threadstore.MetadataDB, rollout.Clock) (threadstore.ThreadStore, error)

func DefaultThreadStoreFactory(ctx context.Context, amadeusRoot string, metadata threadstore.MetadataDB, clock rollout.Clock) (threadstore.ThreadStore, error) {
	return threadlocal.Open(ctx, amadeusRoot, metadata, clock)
}
