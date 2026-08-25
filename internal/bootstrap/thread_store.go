package bootstrap

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/thread"
	threadlocal "github.com/Godric-W/Amadeus/internal/thread/local"
)

type ThreadStoreFactory func(context.Context, string) (thread.ThreadStore, error)

func DefaultThreadStoreFactory(ctx context.Context, amadeusRoot string) (thread.ThreadStore, error) {
	return threadlocal.Open(ctx, amadeusRoot, nil)
}
