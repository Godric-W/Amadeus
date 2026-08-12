package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Godric-W/Amadeus/internal/thread"
	threadlocal "github.com/Godric-W/Amadeus/internal/thread/local"
)

type threadStoreFactory func(context.Context, string) (thread.ThreadStore, error)

var persistentIDSequence atomic.Uint64

func defaultThreadStoreFactory(ctx context.Context, amadeusRoot string) (thread.ThreadStore, error) {
	return threadlocal.Open(ctx, amadeusRoot, nil)
}

func nextPersistentID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UTC().UnixNano(), persistentIDSequence.Add(1))
}
