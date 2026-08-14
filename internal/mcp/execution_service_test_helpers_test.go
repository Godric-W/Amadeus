package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type testApprovalCoordinatorKey struct{}
type testPermissionsKey struct{}

func withTestApprovalCoordinator(ctx context.Context, coordinator *policy.ApprovalCoordinator) context.Context {
	return context.WithValue(ctx, testApprovalCoordinatorKey{}, coordinator)
}

func withTestPermissions(ctx context.Context, permissions *policy.SessionPermissionContext) context.Context {
	return context.WithValue(ctx, testPermissionsKey{}, permissions)
}

func testApprovalCoordinator(ctx context.Context) *policy.ApprovalCoordinator {
	if coordinator, ok := ctx.Value(testApprovalCoordinatorKey{}).(*policy.ApprovalCoordinator); ok {
		return coordinator
	}
	coordinator, _ := policy.NewApprovalCoordinator(&mcpApprovalStub{decision: policy.ApprovalDecision{
		Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce,
		Source: policy.ApprovalSourceUser, Reason: "test MCP call approved",
	}})
	return coordinator
}

func executePreparedTool(t testing.TB, ctx context.Context, candidate tool.ToolDefinition, arguments json.RawMessage) (tool.ToolResult, error) {
	t.Helper()
	coordinator := testApprovalCoordinator(ctx)
	registry := tool.NewRegistry()
	if err := registry.RegisterDefinition(candidate); err != nil {
		t.Fatalf("register test tool: %v", err)
	}
	permissions, _ := ctx.Value(testPermissionsKey{}).(*policy.SessionPermissionContext)
	service, err := tool.NewToolExecutionService(registry, tool.NewArgumentValidator(), tool.ToolExecutionServiceOptions{Approvals: coordinator, Permissions: permissions})
	if err != nil {
		t.Fatalf("create test tool service: %v", err)
	}
	execution, err := service.Execute(ctx, tool.NewCall("test-call", candidate.Spec().Name, arguments))
	if err != nil {
		return execution.Output, err
	}
	if execution.Outcome.Error != nil {
		return execution.Output, errors.New(execution.Outcome.Error.Message)
	}
	return execution.Output, nil
}
