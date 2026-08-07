package policy

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type recordingApprovalHandler struct {
	mutex     sync.Mutex
	requests  []ApprovalRequest
	decisions []ApprovalDecision
}

func (handler *recordingApprovalHandler) Decide(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	handler.requests = append(handler.requests, request)
	if len(handler.decisions) == 0 {
		return ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "denied"}, nil
	}
	decision := handler.decisions[0]
	handler.decisions = handler.decisions[1:]
	return decision, nil
}

func TestToolAuthorizerSkipsStructuredTools(t *testing.T) {
	handler := &recordingApprovalHandler{}
	authorizer, err := NewToolAuthorizer(handler, NewSessionApprovalStore())
	if err != nil {
		t.Fatal(err)
	}
	call := tool.NewCall("read-1", "read_file", json.RawMessage(`{"path":"a"}`))
	prepared, _ := tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{{Kind: tool.TargetFilesystem, Access: tool.TargetAccessRead, CanonicalPath: "/tmp/a"}}})
	if err := authorizer.Authorize(context.Background(), tool.Spec{Name: "read_file", SideEffect: tool.SideEffectRead}, prepared); err != nil {
		t.Fatal(err)
	}
	if len(handler.requests) != 0 {
		t.Fatal("structured tool requested operation approval")
	}
}

func TestToolAuthorizerApprovesUnsandboxedCommandAndCachesSession(t *testing.T) {
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved"}}}
	store := NewSessionApprovalStore()
	authorizer, err := NewToolAuthorizer(handler, store)
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedCommand(t, sandboxdomain.IsolationUnsandboxed, false)
	spec := tool.Spec{Name: "execute_command", SideEffect: tool.SideEffectExecute}
	if err := authorizer.Authorize(context.Background(), spec, prepared); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Authorize(context.Background(), spec, prepared); err != nil {
		t.Fatal(err)
	}
	if len(handler.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(handler.requests))
	}
}

func TestToolAuthorizerSkipsSandboxedCommandAndBlocksCatastrophic(t *testing.T) {
	handler := &recordingApprovalHandler{}
	authorizer, _ := NewToolAuthorizer(handler, NewSessionApprovalStore())
	spec := tool.Spec{Name: "execute_command", SideEffect: tool.SideEffectExecute}
	if err := authorizer.Authorize(context.Background(), spec, preparedCommand(t, sandboxdomain.IsolationSandboxed, false)); err != nil {
		t.Fatal(err)
	}
	call := tool.NewCall("cmd-bad", "execute_command", json.RawMessage(`{"command":"rm -rf /"}`))
	prepared, _ := tool.NewPreparedCall(call, tool.PreparedOptions{Command: "rm -rf /", Shell: "/bin/sh", CWD: "/tmp", IsolationMode: string(sandboxdomain.IsolationSandboxed)})
	if err := authorizer.Authorize(context.Background(), spec, prepared); err == nil {
		t.Fatal("catastrophic command allowed")
	}
}

func preparedCommand(t *testing.T, isolation sandboxdomain.IsolationMode, tty bool) tool.PreparedCall {
	t.Helper()
	call := tool.NewCall("cmd-1", "execute_command", json.RawMessage(`{"command":"go test ./..."}`))
	prepared, err := tool.NewPreparedCall(call, tool.PreparedOptions{Command: "go test ./...", Shell: "/bin/sh", CWD: "/tmp", TTY: tty, IsolationMode: string(isolation)})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}
