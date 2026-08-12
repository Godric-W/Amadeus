package main

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/thread"
)

type codingTaskRequest struct {
	invocation agentInvocation
	result     chan error
}

type codingTaskFactory struct {
	mu                 sync.Mutex
	runner             *agentController
	threadID           thread.ID
	configured         config.Config
	baseInvocation     agentInvocation
	requests           chan codingTaskRequest
	extensions         *extensionruntime.Runtime
	sessionPermissions *project.PermissionStore
	sessionApprovals   *policy.SessionApprovalStore
}

type codingSessionTask struct {
	factory *codingTaskFactory
	request codingTaskRequest
	kind    task.Kind
}

func newCodingTaskFactory(runner *agentController, threadID thread.ID, configured config.Config, invocation agentInvocation) (*codingTaskFactory, error) {
	if runner == nil || threadID == "" {
		return nil, errors.New("coding task factory is incomplete")
	}
	return &codingTaskFactory{
		runner: runner, threadID: threadID, configured: configured, baseInvocation: invocation,
		requests: make(chan codingTaskRequest, 1), sessionPermissions: project.NewPermissionStore(),
		sessionApprovals: policy.NewSessionApprovalStore(),
	}, nil
}

func (factory *codingTaskFactory) Prepare(ctx context.Context, invocation agentInvocation) (<-chan error, error) {
	request := codingTaskRequest{invocation: invocation, result: make(chan error, 1)}
	select {
	case factory.requests <- request:
		return request.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (factory *codingTaskFactory) RegularTask(input string) (task.SessionTask, error) {
	request := factory.nextRequest()
	request.invocation.Task = input
	return &codingSessionTask{factory: factory, request: request, kind: task.KindRegular}, nil
}

func (factory *codingTaskFactory) CompactTask() (task.SessionTask, error) {
	request := factory.nextRequest()
	return &codingSessionTask{factory: factory, request: request, kind: task.KindCompact}, nil
}

func (factory *codingTaskFactory) nextRequest() codingTaskRequest {
	select {
	case request := <-factory.requests:
		return request
	default:
		return codingTaskRequest{invocation: factory.baseInvocation, result: make(chan error, 1)}
	}
}

func (factory *codingTaskFactory) ensureExtensions() (*extensionruntime.Runtime, error) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.extensions != nil {
		return factory.extensions, nil
	}
	runtime, err := extensionruntime.New(factory.runner.runtime.amadeusRoot, factory.baseInvocation.Project, extensionruntime.Options{MCPClientFactory: factory.runner.runtime.mcpClientFactory})
	if err != nil {
		return nil, err
	}
	factory.extensions = runtime
	return runtime, nil
}

func (factory *codingTaskFactory) Close() error {
	factory.mu.Lock()
	runtime := factory.extensions
	factory.extensions = nil
	factory.mu.Unlock()
	factory.sessionPermissions.Clear()
	factory.sessionApprovals.Clear()
	if runtime == nil {
		return nil
	}
	return runtime.Close()
}

func (sessionTask *codingSessionTask) Kind() task.Kind {
	return sessionTask.kind
}

func (sessionTask *codingSessionTask) Run(ctx context.Context, host task.Host, turnContext *turn.Context, inputs []task.Input) (task.Result, error) {
	var result task.Result
	var err error
	if sessionTask.kind == task.KindCompact {
		result, err = sessionTask.factory.runner.executeCompactTurn(ctx, sessionTask.factory, host, turnContext)
	} else {
		result, err = sessionTask.factory.runner.executeCodingTurn(ctx, sessionTask.factory, sessionTask.request.invocation, host, turnContext, inputs)
	}
	sessionTask.request.result <- err
	close(sessionTask.request.result)
	return result, err
}

func (sessionTask *codingSessionTask) Abort(context.Context, task.Host, *turn.Context) error {
	return nil
}

var _ io.Closer = (*codingTaskFactory)(nil)
