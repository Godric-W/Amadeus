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

func TestCommandAuthorizerApprovesUnsandboxedCommandAndCachesSession(t *testing.T) {
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved"}}}
	store := NewSessionApprovalStore()
	authorizer, err := NewCommandAuthorizer(handler, store)
	if err != nil {
		t.Fatal(err)
	}
	request := commandRequest(sandboxdomain.IsolationUnsandboxed, false, "go test ./...")
	if err := authorizer.Authorize(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Authorize(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(handler.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(handler.requests))
	}
}

func TestCommandAuthorizerSkipsSandboxedCommandAndBlocksCatastrophic(t *testing.T) {
	handler := &recordingApprovalHandler{}
	authorizer, _ := NewCommandAuthorizer(handler, NewSessionApprovalStore())
	if err := authorizer.Authorize(context.Background(), commandRequest(sandboxdomain.IsolationSandboxed, false, "go test ./...")); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Authorize(context.Background(), commandRequest(sandboxdomain.IsolationSandboxed, false, "rm -rf /")); err == nil {
		t.Fatal("catastrophic command allowed")
	}
}

func commandRequest(isolation sandboxdomain.IsolationMode, tty bool, command string) CommandRequest {
	arguments, _ := json.Marshal(map[string]any{"command": command})
	return CommandRequest{
		Call:          tool.NewCall("cmd-1", "execute_command", arguments),
		Shell:         "/bin/sh",
		Command:       command,
		CWD:           "/tmp",
		TTY:           tty,
		IsolationMode: isolation,
	}
}
