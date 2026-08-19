package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type mcpApprovalStub struct {
	decision policy.ApprovalDecision
	requests []policy.ApprovalRequest
}

func (stub *mcpApprovalStub) Decide(_ context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	stub.requests = append(stub.requests, request.Clone())
	return stub.decision, nil
}

func TestLazyToolsDoNotStartServerUntilExecution(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}, result: RemoteResult{Text: "hello"}}
	starts := 0
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) {
		starts++
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list, call, err := NewLazyTools(manager)
	if err != nil || list == nil || call == nil || starts != 0 {
		t.Fatalf("lazy MCP tools started eagerly: list=%v call=%v starts=%d err=%v", list, call, starts, err)
	}
	result, err := executePreparedTool(t, context.Background(), list, json.RawMessage(`{"server":"demo"}`))
	if err != nil || starts != 1 || !strings.Contains(result.Text, "echo") {
		t.Fatalf("lazy list failed: result=%#v starts=%d err=%v", result, starts, err)
	}
	result, err = executePreparedTool(t, context.Background(), call, json.RawMessage(`{"server":"demo","name":"echo","arguments":{"value":"hi"}}`))
	if err != nil || starts != 1 || client.callCalls != 1 || !strings.Contains(result.Text, "hello") {
		t.Fatalf("lazy call failed: result=%#v starts=%d client=%#v err=%v", result, starts, client, err)
	}
}

func TestLazyToolsRejectToolCallFromStaleSampleBinding(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}, result: RemoteResult{Text: "hello"}}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) {
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list, call, err := NewLazyTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	initial := manager.Binding().Revision
	staleCtx := tool.WithRequestSnapshot(context.Background(), tool.RequestSnapshot{MCPBindingRevision: initial})
	if _, err := executePreparedTool(t, staleCtx, list, json.RawMessage(`{"server":"demo"}`)); err != nil {
		t.Fatalf("list from sampled binding failed: %v", err)
	}
	if _, err := executePreparedTool(t, staleCtx, call, json.RawMessage(`{"server":"demo","name":"echo","arguments":{}}`)); err == nil || !strings.Contains(err.Error(), "changed since model sampling") {
		t.Fatalf("stale sampled binding was accepted: %v", err)
	}
	if client.callCalls != 0 {
		t.Fatalf("stale sampled call reached remote MCP server: %d", client.callCalls)
	}
	currentCtx := tool.WithRequestSnapshot(context.Background(), tool.RequestSnapshot{MCPBindingRevision: manager.Binding().Revision})
	if _, err := executePreparedTool(t, currentCtx, call, json.RawMessage(`{"server":"demo","name":"echo","arguments":{}}`)); err != nil {
		t.Fatalf("current sampled binding was rejected: %v", err)
	}
}

func TestLazyCallApprovalDenialPreventsRemoteCall(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}, result: RemoteResult{Text: "hello"}}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) {
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	approvals := &mcpApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "no"}}
	_, call, err := NewLazyTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestApprovalCoordinator(context.Background(), coordinator)
	ctx = withTestPermissions(ctx, policy.NewSessionPermissionContext())
	if _, err := executePreparedTool(t, ctx, call, json.RawMessage(`{"server":"demo","name":"echo","arguments":{}}`)); err == nil {
		t.Fatal("denied MCP call succeeded")
	}
	if client.callCalls != 0 || len(approvals.requests) != 1 {
		t.Fatalf("denied MCP call reached remote or skipped approval: calls=%d requests=%d", client.callCalls, len(approvals.requests))
	}
}

func TestLazyCallSessionApprovalIsScopedByServerAndTool(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}, {Name: "other", InputSchema: json.RawMessage(`{"type":"object"}`)}}, result: RemoteResult{Text: "hello"}}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}, "other": {Transport: TransportStdio, Command: "other"}}}, func(context.Context, ServerConfig) (Client, error) {
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	approvals := &mcpApprovalStub{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "trusted"}}
	_, call, err := NewLazyTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestApprovalCoordinator(context.Background(), coordinator)
	ctx = withTestPermissions(ctx, policy.NewSessionPermissionContext())
	for _, raw := range []string{
		`{"server":"demo","name":"echo","arguments":{}}`,
		`{"server":"demo","name":"echo","arguments":{}}`,
		`{"server":"demo","name":"other","arguments":{}}`,
	} {
		if _, err := executePreparedTool(t, ctx, call, json.RawMessage(raw)); err != nil {
			t.Fatalf("MCP call %s failed: %v", raw, err)
		}
	}
	if len(approvals.requests) != 2 || client.callCalls != 3 {
		t.Fatalf("unexpected MCP approval/call counts: requests=%d calls=%d", len(approvals.requests), client.callCalls)
	}
}

func TestLazyCallReadOnlyToolDefaultsToAllowAndPreservesMetadata(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "inspect", Description: "Inspect", ReadOnlyHint: true, IdempotentHint: true, SupportsParallelCalls: true, InputSchema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}`)}}, result: RemoteResult{Text: "ok"}}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	_, call, err := NewLazyTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	invocation := tool.Invocation{Call: tool.NewCall("call", "mcp_call", json.RawMessage(`{"server":"demo","name":"inspect","arguments":{"value":"x"}}`))}
	prepared, err := call.Prepare(tool.ToolUseContext{Context: context.Background(), Invocation: invocation}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Permission.Decision != tool.PermissionAllow {
		t.Fatalf("read-only MCP tool requested approval: %#v", prepared.Permission)
	}
	catalog, err := manager.ToolCatalog(context.Background(), "demo")
	if err != nil || len(catalog.Tools) != 1 || !catalog.Tools[0].ReadOnly || !catalog.Tools[0].SupportsParallelCalls || !catalog.Tools[0].Idempotent {
		t.Fatalf("MCP metadata was not preserved: catalog=%#v err=%v", catalog, err)
	}
}

func TestLazyCallRejectsArgumentsOutsideRemoteSchema(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}},"additionalProperties":false}`)}}}
	runtime, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, call, err := NewLazyTools(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), call, json.RawMessage(`{"server":"demo","name":"inspect","arguments":{"wrong":true}}`)); err == nil || !strings.Contains(err.Error(), "do not match input schema") {
		t.Fatalf("invalid MCP schema arguments were accepted: %v", err)
	}
	if client.callCalls != 0 {
		t.Fatalf("invalid schema arguments reached remote server: %d", client.callCalls)
	}
}

func TestLazyMCPRemoteFailureProducesVisibleErrorMetadata(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object"}`)}}, callErrs: []error{errors.New("remote unavailable"), errors.New("remote unavailable")}}
	runtime, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, call, err := NewLazyTools(runtime)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestPermissions(context.Background(), policy.NewSessionPermissionContext())
	output, err := executePreparedTool(t, ctx, call, json.RawMessage(`{"server":"demo","name":"inspect","arguments":{}}`))
	if err == nil || output.Metadata["error_kind"] != "mcp_error" || output.Metadata["server"] != "demo" || output.Metadata["tool"] != "inspect" {
		t.Fatalf("remote MCP failure was not projected as typed result: output=%#v err=%v", output, err)
	}
}
