package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type fileApprovalStub struct {
	decision policy.ApprovalDecision
	requests []policy.ApprovalRequest
}

func (stub *fileApprovalStub) Decide(_ context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	stub.requests = append(stub.requests, request.Clone())
	return stub.decision, nil
}

func newFileToolsTest(t *testing.T, decision policy.ApprovalDecision) (*FileTools, *fileApprovalStub, string) {
	t.Helper()
	rootPath := t.TempDir()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD:     rootPath,
		Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{rootPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stub := &fileApprovalStub{decision: decision}
	files, err := NewFileTools(root, FileToolsOptions{FileSystemPolicy: filesystem, Approvals: stub, SessionApprovals: policy.NewFileApprovalStore()})
	if err != nil {
		t.Fatal(err)
	}
	return files, stub, rootPath
}

func TestFileEditRequiresApprovalAndReturnsDiff(t *testing.T) {
	files, stub, rootPath := newFileToolsTest(t, policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "approved"})
	path := filepath.Join(rootPath, "main.go")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := files.EditTool().Handle(context.Background(), tool.Invocation{Call: tool.NewCall("edit-1", "edit", json.RawMessage(`{"path":"main.go","old_string":"before","new_string":"after"}`))})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if len(stub.requests) != 1 || stub.requests[0].Purpose != policy.ApprovalPurposeFile || stub.requests[0].Path != path {
		t.Fatalf("unexpected approval request: %#v", stub.requests)
	}
	if !strings.Contains(result.Metadata["change"].(FileChange).UnifiedDiff, "+after") {
		t.Fatalf("missing diff: %#v", result.Metadata)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "after\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestFileEditRejectsStaleTargetAfterApproval(t *testing.T) {
	files, stub, rootPath := newFileToolsTest(t, policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "approved"})
	path := filepath.Join(rootPath, "main.go")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := files.applyChange
	_ = original
	// The approval callback mutates the file between preview and revalidation.
	stub.decision = policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "approved"}
	stubCallback := &mutatingApproval{stub: stub, path: path}
	files.approvals = stubCallback
	_, err := files.EditTool().Handle(context.Background(), tool.Invocation{Call: tool.NewCall("edit-2", "edit", json.RawMessage(`{"path":"main.go","old_string":"before","new_string":"after"}`))})
	var stale *staleFileError
	if !errors.As(err, &stale) {
		t.Fatalf("expected stale error, got %v", err)
	}
}

type mutatingApproval struct {
	stub *fileApprovalStub
	path string
}

func (approval *mutatingApproval) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	decision, err := approval.stub.Decide(ctx, request)
	if err == nil {
		_ = os.WriteFile(approval.path, []byte("changed\n"), 0o644)
	}
	return decision, err
}

func TestFileWriteSessionApprovalCoversDirectory(t *testing.T) {
	files, stub, rootPath := newFileToolsTest(t, policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "trusted"})
	for _, name := range []string{"one.txt", "two.txt"} {
		_, err := files.WriteTool().Handle(context.Background(), tool.Invocation{Call: tool.NewCall("write-"+name, "write", json.RawMessage(`{"path":"`+name+`","content":"ok"}`))})
		if err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if len(stub.requests) != 1 {
		t.Fatalf("session approval should be reused, requests=%d", len(stub.requests))
	}
	if !files.sessionApprovals.Allows(filepath.Join(rootPath, "nested", "file.txt")) {
		t.Fatal("session directory grant not applied")
	}
}

func TestFileReadIsParallelAndDoesNotAsk(t *testing.T) {
	files, stub, rootPath := newFileToolsTest(t, policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "not expected"})
	if err := os.WriteFile(filepath.Join(rootPath, "read.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := files.ReadTool().Handle(context.Background(), tool.Invocation{Call: tool.NewCall("read-1", "read", json.RawMessage(`{"path":"read.txt"}`))})
	if err != nil || result.Text != "L1:hello\n" {
		t.Fatalf("read failed: %#v %v", result, err)
	}
	if len(stub.requests) != 0 {
		t.Fatalf("read unexpectedly asked for approval: %#v", stub.requests)
	}
}
