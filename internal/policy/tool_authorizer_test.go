package policy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type recordingApprovalHandler struct {
	mutex     sync.Mutex
	requests  []ApprovalRequest
	decisions []ApprovalDecision
	err       error
}

func (handler *recordingApprovalHandler) Decide(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	handler.requests = append(handler.requests, request.Clone())
	if handler.err != nil {
		return ApprovalDecision{}, handler.err
	}
	index := len(handler.requests) - 1
	if index >= len(handler.decisions) {
		return ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: "test default deny"}, nil
	}
	return handler.decisions[index], nil
}

func (handler *recordingApprovalHandler) count() int {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	return len(handler.requests)
}

func TestToolAuthorizerRunsPathPreflightBeforeApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)

	err := authorizer.Authorize(context.Background(), applyPatchToolSpec(), tool.NewCall("write-1", "apply_patch", patchPolicyArguments(t, "*** Begin Patch\n*** Add File: ../outside\n+x\n*** End Patch")))
	if err == nil || !strings.Contains(err.Error(), "path preflight") {
		t.Fatalf("unexpected path preflight result: %v", err)
	}
	if handler.count() != 0 {
		t.Fatalf("path escape reached approval handler %d time(s)", handler.count())
	}

	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root.Path(), "escape")); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}
	err = authorizer.Authorize(context.Background(), applyPatchToolSpec(), tool.NewCall("write-2", "apply_patch", patchPolicyArguments(t, "*** Begin Patch\n*** Add File: escape/file.txt\n+x\n*** End Patch")))
	if err == nil || !strings.Contains(err.Error(), "outside project root") || handler.count() != 0 {
		t.Fatalf("symlink escape did not fail before approval: err=%v calls=%d", err, handler.count())
	}
}

func TestToolAuthorizerPreflightsApplyPatchAndRequiresHighRiskApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)
	validArguments := patchPolicyArguments(t, "*** Begin Patch v1\n*** Add File: added.txt\n+content\n*** End Patch\n")
	if err := authorizer.Authorize(context.Background(), applyPatchToolSpec(), tool.NewCall("patch-allow", "apply_patch", validArguments)); err != nil {
		t.Fatalf("authorize apply_patch: %v", err)
	}
	if handler.count() != 1 || handler.requests[0].Risk != CommandRiskHigh || handler.requests[0].Reason != "tool writes project files" {
		t.Fatalf("unexpected apply_patch approval request: %#v", handler.requests)
	}

	escapeArguments := patchPolicyArguments(t, "*** Begin Patch v1\n*** Add File: ../outside.txt\n+blocked\n*** End Patch\n")
	err := authorizer.Authorize(context.Background(), applyPatchToolSpec(), tool.NewCall("patch-escape", "apply_patch", escapeArguments))
	if err == nil || !strings.Contains(err.Error(), "preflight patch path") || handler.count() != 1 {
		t.Fatalf("patch escape reached approval: err=%v calls=%d", err, handler.count())
	}

	malformedArguments := patchPolicyArguments(t, "not a patch")
	err = authorizer.Authorize(context.Background(), applyPatchToolSpec(), tool.NewCall("patch-malformed", "apply_patch", malformedArguments))
	if err == nil || !strings.Contains(err.Error(), "parse patch document for policy") || handler.count() != 1 {
		t.Fatalf("malformed patch reached approval: err=%v calls=%d", err, handler.count())
	}
}

func TestToolAuthorizerBlocksDangerousCommandBeforeApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)

	err := authorizer.Authorize(context.Background(), executeToolSpec(), tool.NewCall("exec-1", "execute_command", json.RawMessage(`{"command":"rm -rf /","cwd":"."}`)))
	var denied *ToolDeniedError
	if !errors.As(err, &denied) || denied.Risk != CommandRiskBlocked || denied.Source != ApprovalSourcePolicy {
		t.Fatalf("unexpected blocked command result: %#v err=%v", denied, err)
	}
	if handler.count() != 0 {
		t.Fatalf("blocked command reached approval handler %d time(s)", handler.count())
	}
}

func TestToolAuthorizerRequiresApprovalForLowRiskCommand(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)
	if err := authorizer.Authorize(context.Background(), executeToolSpec(), tool.NewCall("exec-low", "execute_command", json.RawMessage(`{"command":"git status --short"}`))); err != nil {
		t.Fatalf("authorize low-risk command: %v", err)
	}
	if handler.count() != 1 || handler.requests[0].Risk != CommandRiskModerate {
		t.Fatalf("low-risk command approval mismatch: %#v", handler.requests)
	}
}

func TestToolAuthorizerDeniesCommandPathEscapeBeforeApproval(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)
	err := authorizer.Authorize(context.Background(), executeToolSpec(), tool.NewCall("exec-escape", "execute_command", json.RawMessage(`{"command":"cat ../docs/design.md"}`)))
	if !errors.Is(err, ErrToolDenied) || handler.count() != 0 {
		t.Fatalf("command path escape was not denied before approval: err=%v calls=%d", err, handler.count())
	}
}

func TestToolAuthorizerClassifiesNoneReadAndNetworkTools(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)

	for _, spec := range []tool.Spec{
		{Name: "status", SideEffect: tool.SideEffectNone},
		{Name: "read_context", SideEffect: tool.SideEffectRead},
	} {
		if err := authorizer.Authorize(context.Background(), spec, tool.NewCall(spec.Name+"-1", spec.Name, json.RawMessage(`{}`))); err != nil {
			t.Fatalf("authorize %s tool: %v", spec.SideEffect, err)
		}
	}
	if handler.count() != 0 {
		t.Fatalf("none/read tools requested approval %d time(s)", handler.count())
	}

	network := tool.Spec{Name: "mcp_lookup", SideEffect: tool.SideEffectNetwork}
	if err := authorizer.Authorize(context.Background(), network, tool.NewCall("network-1", network.Name, json.RawMessage(`{"query":"status"}`))); err != nil {
		t.Fatalf("authorize network tool: %v", err)
	}
	if handler.count() != 1 || handler.requests[0].Risk != CommandRiskHigh || handler.requests[0].Reason != "tool accesses external systems" {
		t.Fatalf("unexpected network approval request: %#v", handler.requests)
	}
}

func TestToolAuthorizerDescribesMCPTargetWithoutLeakingArguments(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}
	authorizer := newTestToolAuthorizer(t, root, handler, nil)
	spec := tool.Spec{Name: "mcp_call", SideEffect: tool.SideEffectNetwork}
	arguments := json.RawMessage(`{"server":"demo","name":"echo","arguments":{"api_key":"must-not-leak"}}`)
	if err := authorizer.Authorize(context.Background(), spec, tool.NewCall("mcp-1", "mcp_call", arguments)); err != nil {
		t.Fatalf("authorize MCP call: %v", err)
	}
	if handler.count() != 1 || !strings.Contains(handler.requests[0].Reason, "demo") || !strings.Contains(handler.requests[0].Reason, "echo") {
		t.Fatalf("MCP approval omitted target: %#v", handler.requests)
	}
	if strings.Contains(handler.requests[0].Reason, "must-not-leak") {
		t.Fatalf("MCP approval leaked arguments: %#v", handler.requests[0])
	}
}

func TestToolAuthorizerAppliesApprovalAndExactSessionGrant(t *testing.T) {
	root := newPolicyProjectRoot(t)
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{
		{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved for session"},
		{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "different operation denied"},
	}}
	grants := NewGrantCache()
	authorizer := newTestToolAuthorizer(t, root, handler, grants)
	arguments := json.RawMessage(`{"path":"result.txt","content":"same"}`)
	for _, id := range []string{"write-1", "write-2"} {
		if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall(id, "write_file", arguments)); err != nil {
			t.Fatalf("authorize session-granted call %s: %v", id, err)
		}
	}
	if handler.count() != 1 {
		t.Fatalf("exact session grant did not suppress repeated approval: %d", handler.count())
	}

	err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-3", "write_file", json.RawMessage(`{"path":"result.txt","content":"different"}`)))
	if err != nil || handler.count() != 1 {
		t.Fatalf("same tool did not reuse session grant: err=%v calls=%d", err, handler.count())
	}
}

func TestToolAuthorizerPropagatesApprovalFailuresAndInvalidDecisions(t *testing.T) {
	root := newPolicyProjectRoot(t)
	expected := errors.New("handler failed")
	authorizer := newTestToolAuthorizer(t, root, &recordingApprovalHandler{err: expected}, nil)
	err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-error", "write_file", json.RawMessage(`{"path":"result.txt","content":"x"}`)))
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected handler error: %v", err)
	}

	authorizer = newTestToolAuthorizer(t, root, &recordingApprovalHandler{decisions: []ApprovalDecision{{Outcome: ApprovalAllow}}}, nil)
	err = authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-invalid", "write_file", json.RawMessage(`{"path":"result.txt","content":"x"}`)))
	if err == nil || !strings.Contains(err.Error(), "validate tool approval decision") {
		t.Fatalf("unexpected invalid decision result: %v", err)
	}
}

func TestToolAuthorizerValidatesConstructionAndCalls(t *testing.T) {
	root := newPolicyProjectRoot(t)
	if authorizer, err := NewToolAuthorizer(root, nil, nil); err == nil || authorizer != nil {
		t.Fatalf("unexpected nil-handler constructor result: authorizer=%#v err=%v", authorizer, err)
	}
	if authorizer, err := NewToolAuthorizer(project.Root{}, &recordingApprovalHandler{}, nil); err == nil || authorizer != nil {
		t.Fatalf("unexpected empty-root constructor result: authorizer=%#v err=%v", authorizer, err)
	}

	var authorizer *ToolAuthorizer
	if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("id", "write_file", json.RawMessage(`{}`))); err == nil {
		t.Fatal("nil authorizer did not fail")
	}
	authorizer = newTestToolAuthorizer(t, root, &recordingApprovalHandler{}, nil)
	if err := authorizer.Authorize(nil, writeToolSpec(), tool.NewCall("id", "write_file", json.RawMessage(`{}`))); err == nil {
		t.Fatal("nil context did not fail")
	}
	if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("id", "other", json.RawMessage(`{}`))); err == nil {
		t.Fatal("mismatched call/spec did not fail")
	}
}

func newPolicyProjectRoot(t *testing.T) project.Root {
	t.Helper()
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create policy project root: %v", err)
	}
	return root
}

func newTestToolAuthorizer(t *testing.T, root project.Root, handler ApprovalHandler, grants *GrantCache) *ToolAuthorizer {
	t.Helper()
	authorizer, err := NewToolAuthorizer(root, handler, grants)
	if err != nil {
		t.Fatalf("create tool authorizer: %v", err)
	}
	return authorizer
}

func writeToolSpec() tool.Spec {
	return tool.Spec{Name: "write_file", SideEffect: tool.SideEffectWrite}
}

func executeToolSpec() tool.Spec {
	return tool.Spec{Name: "execute_command", SideEffect: tool.SideEffectExecute}
}

func applyPatchToolSpec() tool.Spec {
	return tool.Spec{Name: "apply_patch", SideEffect: tool.SideEffectWrite}
}

func patchPolicyArguments(t *testing.T, patch string) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("encode patch arguments: %v", err)
	}
	return arguments
}

func allowOnceDecision() ApprovalDecision {
	return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "approved once"}
}
