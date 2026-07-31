package policy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeApprovalHandler struct {
	decision ApprovalDecision
	err      error
	request  ApprovalRequest
}

func (handler *fakeApprovalHandler) Decide(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	handler.request = request.Clone()
	return handler.decision, handler.err
}

func TestApprovalRequestCanonicalizesArgumentsAndTracksHash(t *testing.T) {
	request, err := NewApprovalRequest("request-1", "execute_command", json.RawMessage(`{"timeout_ms":1000,"command":"go test ./..."}`), CommandRiskModerate, "tests execute project code")
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
}

func TestApprovalRequestRejectsInvalidState(t *testing.T) {
	for _, arguments := range []string{"[]", "null", `{"a":1}{"b":2}`, "{"} {
		if _, err := NewApprovalRequest("id", "tool", json.RawMessage(arguments), CommandRiskHigh, "reason"); err == nil {
			t.Fatalf("expected invalid arguments %q", arguments)
		}
	}
	valid, _ := NewApprovalRequest("id", "tool", json.RawMessage(`{"a":1}`), CommandRiskHigh, "reason")
	tests := []struct {
		mutate   func(ApprovalRequest) ApprovalRequest
		contains string
	}{
		{func(r ApprovalRequest) ApprovalRequest { r.ID = ""; return r }, "ID"},
		{func(r ApprovalRequest) ApprovalRequest { r.ToolName = ""; return r }, "tool name"},
		{func(r ApprovalRequest) ApprovalRequest { r.Risk = "unknown"; return r }, "risk"},
		{func(r ApprovalRequest) ApprovalRequest { r.Reason = ""; return r }, "reason"},
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
		{Outcome: ApprovalAllow, Scope: ApprovalAlways, Source: ApprovalSourceUser, Reason: "approved always"},
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

func TestApprovalHandlerPortPassesStructuredRequest(t *testing.T) {
	request, _ := NewApprovalRequest("id", "write_file", json.RawMessage(`{"path":"a.txt"}`), CommandRiskHigh, "writes project file")
	want := ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "trusted"}
	handler := &fakeApprovalHandler{decision: want}
	var port ApprovalHandler = handler
	got, err := port.Decide(context.Background(), request)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(handler.request, request) {
		t.Fatalf("unexpected handler result: got=%#v request=%#v err=%v", got, handler.request, err)
	}
	handler.err = errors.New("unavailable")
	if _, err := port.Decide(context.Background(), request); !errors.Is(err, handler.err) {
		t.Fatalf("unexpected handler error: %v", err)
	}
}
