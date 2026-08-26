package bootstrap

import (
	"context"
	"errors"
	"time"

	threadmanager "github.com/Godric-W/Amadeus/internal/threadmanager"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func ListThreads(ctx context.Context, amadeusRoot string, dependencies Dependencies, query threadstore.ListQuery) (threads []threadstore.StoredThread, listErr error) {
	factory := dependencies.ThreadStore
	if factory == nil {
		factory = DefaultThreadStoreFactory
	}
	store, err := factory(ctx, amadeusRoot)
	if err != nil {
		return nil, err
	}
	nextID := dependencies.NextID
	if nextID == nil {
		nextID = NextPersistentID
	}
	manager, err := threadmanager.New(ctx, store, threadmanager.SharedServices{Clock: dependencies.Clock, NextID: nextID})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		listErr = errors.Join(listErr, manager.Close(closeCtx))
	}()
	return manager.ListThreads(ctx, query)
}
