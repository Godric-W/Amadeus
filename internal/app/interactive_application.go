package app

import (
	"context"
	"errors"
	"sync"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/protocol"
	threadmanager "github.com/Godric-W/Amadeus/internal/threadmanager"
)

type InteractiveOptions struct {
	Workspace           *ThreadWorkspace
	Configuration       agentsession.Configuration
	MaxUserMessageBytes int
}

type InteractiveApplication struct {
	workspace           *ThreadWorkspace
	configuration       agentsession.Configuration
	maxUserMessageBytes int
	operationMu         sync.Mutex

	mu               sync.RWMutex
	active           *threadmanager.AmadeusThread
	generation       uint64
	attachmentCancel context.CancelFunc
	phase            string
	title            string
	usage            protocol.TokenCountEvent

	ctx    context.Context
	cancel context.CancelFunc
	events chan InteractiveEvent
}

func NewInteractiveApplication(parent context.Context, options InteractiveOptions) (*InteractiveApplication, error) {
	if parent == nil || options.Workspace == nil {
		return nil, errors.New("interactive application options are incomplete")
	}
	ctx, cancel := context.WithCancel(parent)
	return &InteractiveApplication{
		workspace: options.Workspace, configuration: options.Configuration,
		maxUserMessageBytes: options.MaxUserMessageBytes,
		ctx:                 ctx, cancel: cancel, events: make(chan InteractiveEvent, 256), phase: "idle",
	}, nil
}

func (application *InteractiveApplication) Start(ctx context.Context) (ThreadViewSnapshot, error) {
	active, err := application.workspace.EnsureCurrent(ctx, application.configuration)
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	snapshot, err := application.snapshot(ctx, active, application.nextGeneration())
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	application.installAttachment(active, snapshot)
	return snapshot, nil
}

func (application *InteractiveApplication) Events() <-chan InteractiveEvent {
	if application == nil {
		return nil
	}
	return application.events
}
