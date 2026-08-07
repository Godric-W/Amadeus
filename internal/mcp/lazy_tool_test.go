package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestLazyToolsDoNotStartServerUntilExecution(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}, result: RemoteResult{Text: "hello"}}
	starts := 0
	manager, err := NewManager(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) {
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
	manager, err := NewManager(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) {
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list, call, err := NewLazyTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	initial := manager.BindingSnapshot().Revision
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
	currentCtx := tool.WithRequestSnapshot(context.Background(), tool.RequestSnapshot{MCPBindingRevision: manager.BindingSnapshot().Revision})
	if _, err := executePreparedTool(t, currentCtx, call, json.RawMessage(`{"server":"demo","name":"echo","arguments":{}}`)); err != nil {
		t.Fatalf("current sampled binding was rejected: %v", err)
	}
}
