package bootstrap

import (
	"context"
	"errors"
	"time"

	threadmanager "github.com/Godric-W/Amadeus/internal/threadmanager"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func ListThreads(ctx context.Context, amadeusRoot string, dependencies Dependencies, query threadstore.ListQuery) (threads []threadstore.StoredThread, listErr error) {
	clock := dependencies.Clock
	if clock == nil {
		clock = time.Now
	}
	stateFactory := dependencies.StateRuntime
	if stateFactory == nil {
		stateFactory = DefaultStateRuntimeFactory
	}
	stateRuntime, err := stateFactory(ctx, amadeusRoot, clock)
	if err != nil {
		return nil, err
	}
	extensions, goalService, err := buildExtensions(stateRuntime, clock)
	if err != nil {
		_ = stateRuntime.Close()
		return nil, err
	}
	factory := dependencies.ThreadStore
	if factory == nil {
		factory = DefaultThreadStoreFactory
	}
	store, err := factory(ctx, amadeusRoot, stateRuntime.Threads(), clock)
	if err != nil {
		_ = stateRuntime.Close()
		return nil, err
	}
	nextID := dependencies.NextID
	if nextID == nil {
		nextID = NextPersistentID
	}
	manager, err := threadmanager.New(ctx, store, threadmanager.SharedServices{Clock: clock, NextID: nextID, State: stateRuntime, Extensions: extensions, GoalService: goalService})
	if err != nil {
		_ = store.Close()
		_ = stateRuntime.Close()
		return nil, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		listErr = errors.Join(listErr, manager.Close(closeCtx))
	}()
	return manager.ListThreads(ctx, query)
}
