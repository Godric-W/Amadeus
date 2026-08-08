package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type permissionApprovalHandler struct {
	decision policy.ApprovalDecision
	requests []policy.ApprovalRequest
}

func (handler *permissionApprovalHandler) Decide(_ context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	handler.requests = append(handler.requests, request)
	return handler.decision, nil
}

func TestRequestPermissionsGrantsRunThenRequiresOriginalToolRetry(t *testing.T) {
	workspace, external := t.TempDir(), t.TempDir()
	runStore, sessionStore := project.NewPermissionStore(), project.NewPermissionStore()
	policyValue, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: workspace, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{workspace}},
		RunPermissions: runStore, SessionPermissions: sessionStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := project.NewRoot(workspace)
	patchOptions := patchExecutorDefaults()
	patchOptions.FileSystemPolicy = policyValue
	patchTool, err := NewApplyPatch(root, ApplyPatchOptions{Executor: patchOptions})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(external, "created.txt")
	patchCall := tool.NewCall("patch-1", "apply_patch", permissionJSON(t, map[string]any{"patch": "*** Begin Patch\n*** Add File: " + target + "\n+created\n*** End Patch"}))
	if _, err := patchTool.Handle(context.Background(), tool.Invocation{Call: patchCall, Source: tool.ToolCallSourceModel}); err == nil {
		t.Fatal("outside write did not require permission")
	}
	handler := &permissionApprovalHandler{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalRun, Source: policy.ApprovalSourceUser, Reason: "approved"}}
	requestTool, err := NewRequestPermissions(RequestPermissionsOptions{Policy: policyValue, RunPermissions: runStore, SessionPermissions: sessionStore, Approvals: handler})
	if err != nil {
		t.Fatal(err)
	}
	requestCall := tool.NewCall("permission-1", "request_permissions", permissionJSON(t, map[string]any{"writable_roots": []string{external}, "reason": "create requested output"}))
	if _, err := requestTool.Handle(context.Background(), tool.Invocation{Call: requestCall, Source: tool.ToolCallSourceModel}); err != nil {
		t.Fatal(err)
	}
	if len(handler.requests) != 1 || handler.requests[0].Purpose != policy.ApprovalPurposePermission {
		t.Fatalf("unexpected permission request: %#v", handler.requests)
	}
	if _, err := patchTool.Handle(context.Background(), tool.Invocation{Call: patchCall, Source: tool.ToolCallSourceModel}); err != nil {
		t.Fatalf("new original call was not authorized: %v", err)
	}
}

func TestRequestPermissionsAuditFailureDoesNotGrant(t *testing.T) {
	workspace, external := t.TempDir(), t.TempDir()
	runStore, sessionStore := project.NewPermissionStore(), project.NewPermissionStore()
	policyValue, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: workspace, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{workspace}},
		RunPermissions: runStore, SessionPermissions: sessionStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := &permissionApprovalHandler{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "approved"}}
	want := errors.New("audit unavailable")
	requestTool, err := NewRequestPermissions(RequestPermissionsOptions{
		Policy: policyValue, RunPermissions: runStore, SessionPermissions: sessionStore, Approvals: handler,
		Audit: failingPermissionAuditSink{err: want},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestCall := tool.NewCall("permission-audit", "request_permissions", permissionJSON(t, map[string]any{"writable_roots": []string{external}, "reason": "create requested output"}))
	if _, err := requestTool.Handle(context.Background(), tool.Invocation{Call: requestCall, Source: tool.ToolCallSourceModel}); !errors.Is(err, want) {
		t.Fatalf("unexpected audit failure: %v", err)
	}
	if roots := runStore.Snapshot().WritableRoots; len(roots) != 0 {
		t.Fatalf("audit failure granted run roots: %#v", roots)
	}
	if roots := sessionStore.Snapshot().WritableRoots; len(roots) != 0 {
		t.Fatalf("audit failure granted session roots: %#v", roots)
	}
}

type failingPermissionAuditSink struct{ err error }

func (sink failingPermissionAuditSink) Write(context.Context, audit.Record) error { return sink.err }

func permissionJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
