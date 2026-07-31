package policy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestToolAuthorizerAuditsAllowDenyAndError(t *testing.T) {
	root := newPolicyProjectRoot(t)
	sink := audit.NewMemorySink()
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{{
		Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved for session",
	}}}
	times := []time.Time{
		time.Unix(100, 0), time.Unix(100, 0).Add(25 * time.Millisecond),
		time.Unix(101, 0), time.Unix(101, 0).Add(10 * time.Millisecond),
		time.Unix(102, 0), time.Unix(102, 0).Add(5 * time.Millisecond),
	}
	index := 0
	authorizer, err := NewToolAuthorizerWithOptions(root, handler, ToolAuthorizerOptions{
		Audit: sink, SessionID: "session-1", Now: func() time.Time {
			value := times[index]
			index++
			return value
		},
	})
	if err != nil {
		t.Fatalf("create audited authorizer: %v", err)
	}

	if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-audit", "write_file", json.RawMessage(`{"path":"result.txt","content":"secret body is hashed only"}`))); err != nil {
		t.Fatalf("authorize audited write: %v", err)
	}
	blockedErr := authorizer.Authorize(context.Background(), executeToolSpec(), tool.NewCall("blocked-audit", "execute_command", json.RawMessage(`{"command":"rm -rf /"}`)))
	if !errors.Is(blockedErr, ErrToolDenied) {
		t.Fatalf("unexpected blocked authorization: %v", blockedErr)
	}
	pathErr := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("error-audit", "write_file", json.RawMessage(`{"path":"../outside","content":"x"}`)))
	if pathErr == nil {
		t.Fatal("expected path preflight error")
	}

	records := sink.Snapshot()
	if len(records) != 3 {
		t.Fatalf("unexpected audit record count: %#v", records)
	}
	if records[0].Outcome != audit.OutcomeAllow || records[0].Source != string(ApprovalSourceUser) || records[0].Scope != string(ApprovalSession) || records[0].DurationMS != 25 || records[0].SessionID != "session-1" || records[0].ArgumentsSHA256 == "" {
		t.Fatalf("unexpected allow audit: %#v", records[0])
	}
	if records[1].Outcome != audit.OutcomeDeny || records[1].Source != string(ApprovalSourcePolicy) || records[1].Risk != string(CommandRiskBlocked) || records[1].DurationMS != 10 {
		t.Fatalf("unexpected deny audit: %#v", records[1])
	}
	if records[2].Outcome != audit.OutcomeError || records[2].Source != string(ApprovalSourcePolicy) || records[2].DurationMS != 5 {
		t.Fatalf("unexpected error audit: %#v", records[2])
	}
}

func TestToolAuthorizerFailsClosedWhenAuditWriteFails(t *testing.T) {
	root := newPolicyProjectRoot(t)
	sink := audit.NewMemorySink()
	expected := errors.New("audit unavailable")
	sink.SetError(expected)
	authorizer, err := NewToolAuthorizerWithOptions(root, &recordingApprovalHandler{decisions: []ApprovalDecision{allowOnceDecision()}}, ToolAuthorizerOptions{Audit: sink})
	if err != nil {
		t.Fatalf("create audited authorizer: %v", err)
	}
	err = authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall("write-audit-error", "write_file", json.RawMessage(`{"path":"result.txt","content":"x"}`)))
	if !errors.Is(err, expected) {
		t.Fatalf("unexpected audit failure: %v", err)
	}
}

func TestToolAuthorizerPublishesApprovalEventsIncludingGrantSource(t *testing.T) {
	root := newPolicyProjectRoot(t)
	events := event.NewMemorySink()
	handler := &recordingApprovalHandler{decisions: []ApprovalDecision{{
		Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "approved for session",
	}}}
	authorizer, err := NewToolAuthorizerWithOptions(root, handler, ToolAuthorizerOptions{Events: events})
	if err != nil {
		t.Fatalf("create eventful authorizer: %v", err)
	}
	arguments := json.RawMessage(`{"path":"result.txt","content":"same"}`)
	for _, id := range []string{"approval-1", "approval-2"} {
		if err := authorizer.Authorize(context.Background(), writeToolSpec(), tool.NewCall(id, "write_file", arguments)); err != nil {
			t.Fatalf("authorize eventful call %s: %v", id, err)
		}
	}
	published := events.Snapshot()
	if len(published) != 4 {
		t.Fatalf("unexpected approval event count: %#v", published)
	}
	for _, index := range []int{0, 2} {
		requested, ok := published[index].(event.ApprovalRequested)
		if !ok || requested.ToolName != "write_file" || requested.Risk != string(CommandRiskHigh) {
			t.Fatalf("unexpected approval requested event %d: %#v", index, published[index])
		}
	}
	first, ok := published[1].(event.ApprovalResolved)
	if !ok || first.Source != string(ApprovalSourceUser) || first.Scope != string(ApprovalSession) {
		t.Fatalf("unexpected user approval resolved event: %#v", published[1])
	}
	second, ok := published[3].(event.ApprovalResolved)
	if !ok || second.Source != string(ApprovalSourceGrant) || second.Outcome != string(ApprovalAllow) {
		t.Fatalf("unexpected grant approval resolved event: %#v", published[3])
	}
}
