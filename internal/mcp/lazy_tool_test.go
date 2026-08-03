package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
	result, err := list.Execute(context.Background(), json.RawMessage(`{"server":"demo"}`))
	if err != nil || starts != 1 || !strings.Contains(result.Text, "echo") {
		t.Fatalf("lazy list failed: result=%#v starts=%d err=%v", result, starts, err)
	}
	result, err = call.Execute(context.Background(), json.RawMessage(`{"server":"demo","name":"echo","arguments":{"value":"hi"}}`))
	if err != nil || starts != 1 || client.callCalls != 1 || !strings.Contains(result.Text, "hello") {
		t.Fatalf("lazy call failed: result=%#v starts=%d client=%#v err=%v", result, starts, client, err)
	}
}
