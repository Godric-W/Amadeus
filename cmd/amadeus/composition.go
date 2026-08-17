package main

import (
	"context"
	"fmt"
	"os"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/thread"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

func (runner *agentController) ensureWorkspace(ctx context.Context, invocation agentInvocation) (*app.ThreadWorkspace, config.Config, error) {
	runner.workspaceMu.Lock()
	defer runner.workspaceMu.Unlock()
	configured, _, err := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
	if err != nil {
		return nil, config.Config{}, err
	}
	if err := config.Validate(configured); err != nil {
		return nil, config.Config{}, err
	}
	if runner.workspace != nil {
		return runner.workspace, configured, nil
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
	modelMessages, err := internalprompt.LoadModelMessages()
	if err != nil {
		_ = store.Close()
		return nil, config.Config{}, err
	}
	lifecycleCtx := runner.lifecycleCtx
	if lifecycleCtx == nil {
		lifecycleCtx = ctx
	}
	newBuilder := func() (*agentsession.ServicesBuilder, error) {
		auditFactory := runner.runtime.auditSinkFactory
		if auditFactory == nil {
			auditFactory = defaultAuditSinkFactory(runner.runtime.lookupEnv, os.UserHomeDir)
		}
		options := agentsession.ServicesOptions{
			Config: configured, Project: invocation.Project, WorkspaceRoots: append([]string(nil), invocation.WorkspaceRoots...),
			AmadeusRoot: runner.runtime.amadeusRoot, MCPClientFactory: runner.runtime.mcpClientFactory,
			WebFetcher: runner.runtime.webFetcher, WebSearch: runner.runtime.webSearch,
			AuditFactory: agentsession.AuditFactory(auditFactory), ModelMessages: modelMessages, Clock: clock,
		}
		if runner.runtime.llmClientFactory != nil {
			options.ClientFactory = func(providerName string, providerConfig config.ProviderConfig) (llm.Client, error) {
				return runner.runtime.llmClientFactory(providerName, providerConfig)
			}
		}
		return agentsession.NewServicesBuilder(options)
	}
	manager, err := threadmanager.New(lifecycleCtx, store, threadmanager.SharedServices{
		Clock: clock, NextID: idFactory,
		NewSessionSetup: func(id thread.ID) (agentsession.SessionSetup, error) {
			builder, err := newBuilder()
			if err != nil {
				return agentsession.SessionSetup{}, err
			}
			return agentsession.SessionSetup{
				TaskConstructors: agentsession.TaskConstructors{
					Regular: builder.NewRegularTask, Compact: builder.NewCompactTask, Close: builder.Close,
				},
				BuildServices: builder.BuildServices,
			}, nil
		},
	})
	if err != nil {
		_ = store.Close()
		return nil, config.Config{}, err
	}
	workspace, err := app.NewThreadWorkspace(manager)
	if err != nil {
		_ = manager.Close(context.Background())
		return nil, config.Config{}, err
	}
	runner.workspace = workspace
	return workspace, configured, nil
}

func (runner *agentController) currentWorkspace() *app.ThreadWorkspace {
	if runner == nil {
		return nil
	}
	runner.workspaceMu.Lock()
	defer runner.workspaceMu.Unlock()
	return runner.workspace
}

func sessionConfiguration(configured config.Config, invocation agentInvocation) agentsession.Configuration {
	provider := configured.Providers[configured.DefaultProvider]
	mode := turn.ModeKindDefault
	if invocation.RunMode == "plan" {
		mode = turn.ModeKindPlan
	}
	return agentsession.Configuration{
		CWD: invocation.Project.Path(), Provider: configured.DefaultProvider, Model: provider.Model,
		Mode: mode,
	}
}
