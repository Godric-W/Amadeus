package policy

import (
	"encoding/json"
	"testing"
)

func TestGrantCacheSeparatesSessionAndPersistentDecisions(t *testing.T) {
	cache := NewGrantCache()
	sessionRequest := testGrantRequest(t, "session", `{"path":"session.txt"}`)
	persistentRequest := testGrantRequest(t, "persistent", `{"path":"persistent.txt"}`)
	sessionDecision := ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "session allow"}
	persistentDecision := ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalAlways, Source: ApprovalSourceUser, Reason: "persistent deny"}
	if err := cache.Remember(sessionRequest, sessionDecision); err != nil {
		t.Fatalf("remember session decision: %v", err)
	}
	if err := cache.Remember(persistentRequest, persistentDecision); err != nil {
		t.Fatalf("remember persistent decision: %v", err)
	}
	if decision, ok := cache.Lookup(sessionRequest); !ok || !decision.Allowed() || decision.Source != ApprovalSourceGrant {
		t.Fatalf("unexpected session lookup: decision=%#v ok=%v", decision, ok)
	}
	if decision, ok := cache.Lookup(persistentRequest); !ok || decision.Allowed() || decision.Source != ApprovalSourceGrant {
		t.Fatalf("unexpected persistent lookup: decision=%#v ok=%v", decision, ok)
	}

	cache.ClearSession()
	if _, ok := cache.Lookup(sessionRequest); ok {
		t.Fatal("session decision survived ClearSession")
	}
	if decision, ok := cache.Lookup(persistentRequest); !ok || decision.Allowed() {
		t.Fatalf("persistent decision did not survive ClearSession: decision=%#v ok=%v", decision, ok)
	}
}

func TestGrantCacheIgnoresOnceAndValidatesInputs(t *testing.T) {
	cache := NewGrantCache()
	request := testGrantRequest(t, "once", `{"path":"once.txt"}`)
	if err := cache.Remember(request, allowOnceDecision()); err != nil {
		t.Fatalf("remember once decision: %v", err)
	}
	if _, ok := cache.Lookup(request); ok {
		t.Fatal("once decision was cached")
	}
	if err := (*GrantCache)(nil).Remember(request, allowOnceDecision()); err == nil {
		t.Fatal("nil grant cache did not fail")
	}
	invalid := allowOnceDecision()
	invalid.Reason = ""
	if err := cache.Remember(request, invalid); err == nil {
		t.Fatal("invalid decision was cached")
	}
}

func testGrantRequest(t *testing.T, id, arguments string) ApprovalRequest {
	t.Helper()
	request, err := NewApprovalRequest(id, "write_file", json.RawMessage(arguments), CommandRiskHigh, "write operation")
	if err != nil {
		t.Fatalf("create grant request: %v", err)
	}
	return request
}
