package main

import (
	"context"
	"fmt"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	agenttask "github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

func (runner *agentController) ensureThreadManager(ctx context.Context, invocation agentInvocation) (*threadmanager.ThreadManager, config.Config, error) {
	runner.threadMutex.Lock()
	defer runner.threadMutex.Unlock()
	configured, _, err := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
	if err != nil {
		return nil, config.Config{}, err
	}
	if err := config.Validate(configured); err != nil {
		return nil, config.Config{}, err
	}
	if runner.threadManager != nil {
		return runner.threadManager, configured, nil
	}
	if runner.runtime.rootErr != nil {
		return nil, config.Config{}, fmt.Errorf("resolve Amadeus root for thread store: %w", runner.runtime.rootErr)
	}
	factory := runner.runtime.threadStoreFactory
	if factory == nil {
		factory = defaultThreadStoreFactory
	}
	store, err := factory(ctx, runner.runtime.amadeusRoot)
	if err != nil {
		return nil, config.Config{}, err
	}
	idFactory := runner.runtime.persistentIDFactory
	if idFactory == nil {
		idFactory = nextPersistentID
	}
	baseIDFactory := idFactory
	idFactory = func(kind string) string {
		if kind == "turn" && runner.runtime.turnIDFactory != nil {
			return runner.runtime.turnIDFactory()
		}
		return baseIDFactory(kind)
	}
	clock := runner.runtime.now
	if clock == nil {
		clock = time.Now
	}
	lifecycleCtx := runner.lifecycleCtx
	if lifecycleCtx == nil {
		lifecycleCtx = ctx
	}
	manager, err := threadmanager.New(lifecycleCtx, store, threadmanager.SharedServices{
		Clock: clock, NextID: idFactory,
		NewTaskFactory: func(id thread.ID) (agenttask.Factory, error) {
			factory, createErr := newCodingTaskFactory(runner, id, configured, invocation)
			if createErr != nil {
				return nil, createErr
			}
			runner.threadMutex.Lock()
			runner.taskFactories[id] = factory
			runner.threadMutex.Unlock()
			return factory, nil
		},
	})
	if err != nil {
		_ = store.Close()
		return nil, config.Config{}, err
	}
	runner.threadManager = manager
	return manager, configured, nil
}

func sessionConfiguration(configured config.Config, invocation agentInvocation) agentsession.Configuration {
	provider := configured.Providers[configured.DefaultProvider]
	mode := turn.PermissionModeDefault
	if invocation.RunMode == "plan" {
		mode = turn.PermissionModePlan
	}
	assets, _ := internalprompt.LoadAssets()
	return agentsession.Configuration{
		CWD: invocation.Project.Path(), Provider: configured.DefaultProvider, Model: provider.Model,
		PermissionMode: mode, BaseInstructions: assets.Base,
	}
}

func (runner *agentController) currentThreadMetadata(ctx context.Context) (state.StoredThread, error) {
	runner.threadMutex.Lock()
	manager := runner.threadManager
	current := runner.currentThread
	runner.threadMutex.Unlock()
	if manager == nil || current == nil {
		return state.StoredThread{}, state.ErrNotFound
	}
	threads, err := manager.ListThreads(ctx, state.ListQuery{IncludeArchived: true})
	if err != nil {
		return state.StoredThread{}, err
	}
	for _, metadata := range threads {
		if metadata.ID == current.ID() {
			return metadata, nil
		}
	}
	return state.StoredThread{}, state.ErrNotFound
}
