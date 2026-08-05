package policy

import (
	"encoding/json"
	"testing"
)

func TestGrantCacheReusesSessionDecisionByToolName(t *testing.T) {
	cache := NewGrantCache()
	sessionRequest := testGrantRequest(t, "session", `{"path":"session.txt"}`)
	otherArguments := testGrantRequest(t, "other", `{"path":"other.txt"}`)
	sessionDecision := ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "session allow"}
	if err := cache.Remember(sessionRequest, sessionDecision); err != nil {
		t.Fatalf("remember session decision: %v", err)
	}
	if decision, ok := cache.Lookup(sessionRequest); !ok || !decision.Allowed() || decision.Source != ApprovalSourceGrant {
		t.Fatalf("unexpected session lookup: decision=%#v ok=%v", decision, ok)
	}
	if decision, ok := cache.Lookup(otherArguments); !ok || !decision.Allowed() || decision.Source != ApprovalSourceGrant {
		t.Fatalf("session grant was not reused by tool name: decision=%#v ok=%v", decision, ok)
	}

	cache.ClearSession()
	if _, ok := cache.Lookup(sessionRequest); ok {
		t.Fatal("session decision survived ClearSession")
	}
}

func TestGrantCacheScopesMCPSessionDecisionToTarget(t *testing.T) {
	cache := NewGrantCache()
	request := testNamedGrantRequest(t, "mcp-1", "mcp_call", `{"server":"demo","name":"echo","arguments":{"value":"one"}}`)
	decision := ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "session allow"}
	if err := cache.Remember(request, decision); err != nil {
		t.Fatalf("remember MCP session decision: %v", err)
	}

	sameTarget := testNamedGrantRequest(t, "mcp-2", "mcp_call", `{"server":"demo","name":"echo","arguments":{"value":"two"}}`)
	if _, ok := cache.Lookup(sameTarget); !ok {
		t.Fatal("MCP grant was not reused for the same server and tool")
	}
	for _, other := range []ApprovalRequest{
		testNamedGrantRequest(t, "mcp-3", "mcp_call", `{"server":"demo","name":"other","arguments":{}}`),
		testNamedGrantRequest(t, "mcp-4", "mcp_call", `{"server":"other","name":"echo","arguments":{}}`),
	} {
		if _, ok := cache.Lookup(other); ok {
			t.Fatalf("MCP grant escaped its target: %#v", other)
		}
	}

	resource := testNamedGrantRequest(t, "resource-1", "mcp_read_resource", `{"server":"demo","uri":"file:///one"}`)
	if err := cache.Remember(resource, decision); err != nil {
		t.Fatalf("remember MCP resource decision: %v", err)
	}
	if _, ok := cache.Lookup(testNamedGrantRequest(t, "resource-2", "mcp_read_resource", `{"server":"demo","uri":"file:///two"}`)); ok {
		t.Fatal("MCP resource grant escaped its URI")
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
	return testNamedGrantRequest(t, id, "write_file", arguments)
}

func testNamedGrantRequest(t *testing.T, id, name, arguments string) ApprovalRequest {
	t.Helper()
	request, err := NewApprovalRequest(id, name, json.RawMessage(arguments), CommandRiskHigh, "write operation")
	if err != nil {
		t.Fatalf("create grant request: %v", err)
	}
	return request
}
