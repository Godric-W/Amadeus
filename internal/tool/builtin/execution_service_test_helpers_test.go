package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type testApprovalPort struct {
	decision policy.ApprovalDecision
	calls    int
	requests []policy.ApprovalRequest
}

func (toolImpl *testApprovalPort) Decide(_ context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	toolImpl.calls++
	toolImpl.requests = append(toolImpl.requests, request.Clone())
	return toolImpl.decision, nil
}

type testApprovalCoordinatorKey struct{}
type testFileReadStateKey struct{}
type testPermissionsKey struct{}

func withTestApprovalCoordinator(ctx context.Context, coordinator *policy.ApprovalCoordinator) context.Context {
	return context.WithValue(ctx, testApprovalCoordinatorKey{}, coordinator)
}

func testApprovalCoordinator(ctx context.Context) *policy.ApprovalCoordinator {
	if coordinator, ok := ctx.Value(testApprovalCoordinatorKey{}).(*policy.ApprovalCoordinator); ok {
		return coordinator
	}
	coordinator, _ := policy.NewApprovalCoordinator(&testApprovalPort{decision: policy.ApprovalDecision{
		Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce,
		Source: policy.ApprovalSourceUser, Reason: "test tool approved",
	}})
	return coordinator
}

func withTestFileReadState(ctx context.Context, state *tool.FileReadStateStore) context.Context {
	return context.WithValue(ctx, testFileReadStateKey{}, state)
}

func withTestPermissions(ctx context.Context, permissions *policy.SessionPermissionContext) context.Context {
	return context.WithValue(ctx, testPermissionsKey{}, permissions)
}

func executePreparedTool(t testing.TB, ctx context.Context, candidate tool.ToolDefinition, arguments json.RawMessage) (tool.ToolResult, error) {
	t.Helper()
	coordinator := testApprovalCoordinator(ctx)
	registry := tool.NewRegistry()
	spec := candidate.Spec()
	if registerErr := registry.RegisterDefinition(candidate); registerErr != nil {
		t.Fatalf("register test tool: %v", registerErr)
	}
	state, _ := ctx.Value(testFileReadStateKey{}).(*tool.FileReadStateStore)
	permissions, _ := ctx.Value(testPermissionsKey{}).(*policy.SessionPermissionContext)
	service, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{Approvals: coordinator, Permissions: permissions, FileReadState: state})
	if err != nil {
		t.Fatalf("create test tool service: %v", err)
	}
	metadata := tool.InvocationMetadataFromContext(ctx)
	metadata.Source = tool.ToolCallSourceModel
	ctx = tool.WithInvocationMetadata(ctx, metadata)
	execution, err := service.Execute(ctx, tool.NewCall("test-call", spec.Name, arguments))
	if err != nil {
		return execution.Output, err
	}
	if execution.Outcome.Error != nil {
		message := execution.Outcome.Error.Message
		if _, ok := candidate.(*ExecuteCommand); ok {
			if execution.Outcome.Error.Kind == "execution_failed" {
				if execution.Output.Metadata["timed_out"] == true {
					return execution.Output, ErrCommandTimeout
				}
				if code, ok := execution.Output.Metadata["exit_code"].(int); ok && code != 0 {
					return execution.Output, &CommandExitError{ExitCode: code}
				}
			}
		}
		if execution.Outcome.Error.Kind == "target_stale" {
			path := strings.TrimPrefix(message, "file changed since approval: ")
			return execution.Output, &staleFileError{Path: path}
		}
		if message == processdomain.ErrOwnerMismatch.Error() {
			return execution.Output, processdomain.ErrOwnerMismatch
		}
		if execution.Outcome.Status == tool.ToolCallInterrupted {
			return execution.Output, context.Canceled
		}
		return execution.Output, errors.New(message)
	}
	return execution.Output, nil
}
