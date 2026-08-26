package bootstrap

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/threadstore"
	threadlocal "github.com/Godric-W/Amadeus/internal/threadstore/local"
)

type ThreadStoreFactory func(context.Context, string) (threadstore.ThreadStore, error)

func DefaultThreadStoreFactory(ctx context.Context, amadeusRoot string) (threadstore.ThreadStore, error) {
	return threadlocal.Open(ctx, amadeusRoot, nil)
}
