package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/config"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/protocol"
	threadmanager "github.com/Godric-W/Amadeus/internal/threadmanager"
)

type WorkspaceOptions struct {
	AmadeusRoot    string
	Configuration  config.Config
	CWD            string
	WorkspaceRoots []string
	Mode           protocol.ModeKind
	Dependencies   Dependencies
}

type WorkspaceResult struct {
	Workspace     *app.ThreadWorkspace
	Configuration agentsession.Configuration
}

func OpenWorkspace(ctx context.Context, options WorkspaceOptions) (WorkspaceResult, error) {
	if ctx == nil {
		return WorkspaceResult{}, errors.New("bootstrap context is nil")
	}
	if strings.TrimSpace(options.AmadeusRoot) == "" {
		return WorkspaceResult{}, errors.New("Amadeus root directory is empty")
	}
	if err := config.Validate(options.Configuration); err != nil {
		return WorkspaceResult{}, err
	}
	storeFactory := options.Dependencies.ThreadStore
	if storeFactory == nil {
		storeFactory = DefaultThreadStoreFactory
	}
	store, err := storeFactory(ctx, options.AmadeusRoot)
	if err != nil {
		return WorkspaceResult{}, err
	}
	cleanupStore := true
	defer func() {
		if cleanupStore {
			_ = store.Close()
		}
	}()

	modelMessages, err := internalprompt.LoadModelMessages()
	if err != nil {
		return WorkspaceResult{}, err
	}
	compactionAssets, err := internalprompt.LoadCompactionAssets()
	if err != nil {
		return WorkspaceResult{}, err
	}
	auditFactory := options.Dependencies.AuditFactory
	if auditFactory == nil {
		return WorkspaceResult{}, errors.New("bootstrap audit factory is nil")
	}
	clock := options.Dependencies.Clock
	if clock == nil {
		clock = time.Now
	}
	nextID := options.Dependencies.NextID
	if nextID == nil {
		nextID = NextPersistentID
	}
	manager, err := threadmanager.New(context.WithoutCancel(ctx), store, threadmanager.SharedServices{
		Clock: clock, NextID: nextID,
		SessionAdapters: options.Dependencies.sessionAdapters(modelMessages, compactionAssets),
	})
	if err != nil {
		return WorkspaceResult{}, err
	}
	cleanupStore = false
	workspace, err := app.NewThreadWorkspace(manager)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = manager.Close(cleanupCtx)
		return WorkspaceResult{}, err
	}

	mode := options.Mode
	if mode == "" {
		mode = protocol.ModeKindDefault
	}
	if mode != protocol.ModeKindDefault && mode != protocol.ModeKindPlan {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = workspace.Close(cleanupCtx)
		return WorkspaceResult{}, fmt.Errorf("unsupported collaboration mode %q", mode)
	}
	configuration := agentsession.Configuration{
		Runtime:        options.Configuration,
		CWD:            strings.TrimSpace(options.CWD),
		WorkspaceRoots: append([]string(nil), options.WorkspaceRoots...),
		AmadeusRoot:    options.AmadeusRoot,
		Mode:           mode,
	}
	return WorkspaceResult{Workspace: workspace, Configuration: configuration}, nil
}

func CloseWorkspace(workspace *app.ThreadWorkspace) error {
	if workspace == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return workspace.Close(ctx)
}
