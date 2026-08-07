package agentcontext

import (
	"context"
	"errors"
)

var errContextManagerNil = errors.New("context manager is nil")

type Manager struct {
	builder *Builder
	window  *WindowManager
}

func NewManager(estimator Estimator) *Manager {
	return &Manager{builder: NewBuilder(), window: NewContextWindowManager(estimator)}
}

func (manager *Manager) Build(ctx context.Context, input BuildInput) (Envelope, error) {
	if manager == nil || manager.builder == nil {
		return Envelope{}, errContextManagerNil
	}
	return manager.builder.Build(ctx, input)
}

func (manager *Manager) Prepare(ctx context.Context, request WindowRequest) (RequestView, error) {
	if manager == nil || manager.window == nil {
		return RequestView{}, errContextManagerNil
	}
	return manager.window.Prepare(ctx, request)
}

var _ ContextWindowManager = (*Manager)(nil)
