package policy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/filechange"
)

type fakeApprovalPort struct {
	mutex    sync.Mutex
	decision ApprovalDecision
	err      error
	request  ApprovalRequest
	calls    int
}

func (toolImpl *fakeApprovalPort) Decide(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	toolImpl.mutex.Lock()
	defer toolImpl.mutex.Unlock()
	toolImpl.request = request.Clone()
	toolImpl.calls++
	return toolImpl.decision, toolImpl.err
}

func TestApprovalRequestCanonicalizesArgumentsAndTracksHash(t *testing.T) {
	request, err := NewApprovalRequest("request-1", "execute_command", json.RawMessage(`{"timeout_ms":1000,"command":"go test ./..."}`), CommandRiskModerate, testApprovalCause("tests_execute_code"))
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	if string(request.Arguments) != `{"command":"go test ./...","timeout_ms":1000}` || len(request.ArgumentsSHA256) != 64 {
		t.Fatalf("unexpected canonical request: %#v", request)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate approval request: %v", err)
	}
	clone := request.Clone()
	clone.Arguments[0] = 'x'
	if request.Arguments[0] == 'x' {
		t.Fatal("approval request clone shares argument storage")
	}
	request.Diff = &filechange.Preview{Path: "a.txt", Hunks: []filechange.DiffHunk{{Header: "@@", Lines: []string{"-old", "+new"}}}}
	clone = request.Clone()
	clone.Diff.Hunks[0].Lines[0] = "changed"
	if request.Diff.Hunks[0].Lines[0] == "changed" {
		t.Fatal("approval request clone shares diff storage")
	}
}

func TestApprovalRequestRejectsInvalidState(t *testing.T) {
	for _, arguments := range []string{"[]", "null", `{"a":1}{"b":2}`, "{"} {
		if _, err := NewApprovalRequest("id", "tool", json.RawMessage(arguments), CommandRiskHigh, testApprovalCause("test")); err == nil {
			t.Fatalf("expected invalid arguments %q", arguments)
		}
	}
	valid, _ := NewApprovalRequest("id", "tool", json.RawMessage(`{"a":1}`), CommandRiskHigh, testApprovalCause("test"))
	tests := []struct {
		mutate   func(ApprovalRequest) ApprovalRequest
		contains string
	}{
		{func(r ApprovalRequest) ApprovalRequest { r.ID = ""; return r }, "ID"},
		{func(r ApprovalRequest) ApprovalRequest { r.ToolName = ""; return r }, "tool name"},
		{func(r ApprovalRequest) ApprovalRequest { r.Risk = "unknown"; return r }, "risk"},
		{func(r ApprovalRequest) ApprovalRequest { r.Cause.Code = ""; return r }, "cause code"},
		{func(r ApprovalRequest) ApprovalRequest { r.Arguments = json.RawMessage(`{"b":2,"a":1}`); return r }, "not canonical"},
		{func(r ApprovalRequest) ApprovalRequest { r.ArgumentsSHA256 = strings.Repeat("0", 64); return r }, "does not match"},
	}
	for _, test := range tests {
		if err := test.mutate(valid).Validate(); err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Fatalf("unexpected request validation error: %v", err)
		}
	}
}

func TestApprovalDecisionExpressesAllowDenyAndScopes(t *testing.T) {
	for _, decision := range []ApprovalDecision{
		{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "approved once"},
		{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved for session"},
		{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: "blocked"},
	} {
		if err := decision.Validate(); err != nil {
			t.Fatalf("validate decision %#v: %v", decision, err)
		}
		if decision.Allowed() != (decision.Outcome == ApprovalAllow) {
			t.Fatalf("unexpected Allowed result: %#v", decision)
		}
	}
}

func TestApprovalPortPassesStructuredRequest(t *testing.T) {
	request, _ := NewApprovalRequest("id", "write_file", json.RawMessage(`{"path":"a.txt"}`), CommandRiskHigh, testApprovalCause("writes_project_file"))
	want := ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "trusted"}
	port := &fakeApprovalPort{decision: want}
	got, err := port.Decide(context.Background(), request)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(port.request, request) {
		t.Fatalf("unexpected approval port result: got=%#v request=%#v err=%v", got, port.request, err)
	}
	port.err = errors.New("unavailable")
	if _, err := port.Decide(context.Background(), request); !errors.Is(err, port.err) {
		t.Fatalf("unexpected approval port error: %v", err)
	}
}

func TestApprovalCoordinatorOnlyReturnsDecision(t *testing.T) {
	permissions := NewSessionPermissionContext()
	command, ok := NewCommandApprovalKey("go test ./...", "/workspace")
	if !ok {
		t.Fatal("command key rejected")
	}
	approvalPort := &fakeApprovalPort{decision: ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "trusted"}}
	coordinator, err := NewApprovalCoordinator(approvalPort)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewApprovalRequest("command-1", "execute_command", json.RawMessage(`{"command":"go test ./..."}`), CommandRiskHigh, testApprovalCause("run_command"))
	if err != nil {
		t.Fatal(err)
	}
	request.Command = command.Command
	request.CWD = command.CWD
	if _, err := coordinator.Decide(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if permissions.Match(CommandGrant(command)) {
		t.Fatal("approval coordinator unexpectedly applied a session grant")
	}

	oncePort := &fakeApprovalPort{decision: ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourceUser, Reason: "once"}}
	onceCoordinator, err := NewApprovalCoordinator(oncePort)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewCommandApprovalKey("go test ./other", "/workspace")
	onceRequest, _ := NewApprovalRequest("command-2", "execute_command", json.RawMessage(`{"command":"go test ./other"}`), CommandRiskHigh, testApprovalCause("run_command"))
	onceRequest.Command, onceRequest.CWD = other.Command, other.CWD
	if _, err := onceCoordinator.Decide(context.Background(), onceRequest); err != nil {
		t.Fatal(err)
	}
	if permissions.Match(CommandGrant(other)) {
		t.Fatal("allow once unexpectedly created a session grant")
	}
}

func TestApprovalCoordinatorSerializesRequestsWithoutGrantState(t *testing.T) {
	approvalPort := &fakeApprovalPort{decision: ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "trusted"}}
	coordinator, err := NewApprovalCoordinator(approvalPort)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewApprovalRequestForPurpose("call-1", "mcp_call", json.RawMessage(`{"server":"demo","name":"echo"}`), ApprovalPurposeExternal, CommandRiskHigh, ApprovalCause{Kind: ApprovalCauseExternalTool, Code: "mcp_tool"})
	if err != nil {
		t.Fatal(err)
	}
	request.PermissionKey = ExternalGrant("mcp:demo/echo").Key
	var wait sync.WaitGroup
	errorsFound := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, decideErr := coordinator.Decide(context.Background(), request)
			errorsFound <- decideErr
		}()
	}
	wait.Wait()
	close(errorsFound)
	for decideErr := range errorsFound {
		if decideErr != nil {
			t.Fatal(decideErr)
		}
	}
	approvalPort.mutex.Lock()
	calls := approvalPort.calls
	approvalPort.mutex.Unlock()
	if calls != 2 {
		t.Fatalf("approval coordinator did not forward both serialized requests: calls=%d", calls)
	}
}

func testApprovalCause(code string) ApprovalCause {
	return ApprovalCause{Kind: ApprovalCausePolicy, Code: code}
}
