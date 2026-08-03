package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type fakeClient struct {
	tools      []RemoteTool
	result     RemoteResult
	listErrs   []error
	callErrs   []error
	listCalls  int
	callCalls  int
	closeCalls int
}

func (client *fakeClient) ListTools(context.Context) ([]RemoteTool, error) {
	client.listCalls++
	if len(client.listErrs) > 0 {
		err := client.listErrs[0]
		client.listErrs = client.listErrs[1:]
		return nil, err
	}
	return append([]RemoteTool(nil), client.tools...), nil
}

func (client *fakeClient) CallTool(context.Context, string, json.RawMessage) (RemoteResult, error) {
	client.callCalls++
	if len(client.callErrs) > 0 {
		err := client.callErrs[0]
		client.callErrs = client.callErrs[1:]
		return RemoteResult{}, err
	}
	return client.result, nil
}

func (client *fakeClient) Close() error { client.closeCalls++; return nil }

func TestManagerIsLazyReusesClientAndReconnectsOnce(t *testing.T) {
	configured := Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}
	first := &fakeClient{listErrs: []error{errors.New("connection lost")}}
	second := &fakeClient{tools: []RemoteTool{{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	factories := 0
	manager, err := NewManager(configured, func(context.Context, ServerConfig) (Client, error) {
		factories++
		if factories == 1 {
			return first, nil
		}
		return second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if factories != 0 {
		t.Fatal("MCP manager eagerly started a server")
	}
	tools, err := manager.ListTools(context.Background(), "demo")
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	if factories != 2 || first.closeCalls != 1 || !reflect.DeepEqual(tools, second.tools) {
		t.Fatalf("unexpected lazy reconnect lifecycle: factories=%d first=%#v tools=%#v", factories, first, tools)
	}
	if _, err := manager.ListTools(context.Background(), "demo"); err != nil || factories != 2 || second.listCalls != 2 {
		t.Fatalf("MCP client was not reused: err=%v factories=%d client=%#v", err, factories, second)
	}
	if err := manager.Close(); err != nil || second.closeCalls != 1 {
		t.Fatalf("close manager: err=%v second=%#v", err, second)
	}
}

func TestToolAdapterSanitizesSchemaBoundsUntrustedResultAndUsesNetworkApprovalClass(t *testing.T) {
	client := &fakeClient{result: RemoteResult{Text: "abcdefgh", IsError: false}}
	manager, err := NewManager(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewToolAdapter("demo", RemoteTool{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object","title":"ignored","properties":{"value":{"type":"string","format":"uri"}}}`)}, manager, AdapterOptions{MaxResultBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Execute(context.Background(), json.RawMessage(`{"value":"ok"}`))
	if err != nil || adapter.Spec().Name != "mcp__demo__echo" || adapter.Spec().SideEffect != "network" || !result.Partial || !contains(result.Text, "Untrusted external MCP result") || contains(string(adapter.Spec().InputSchema), "title") || contains(string(adapter.Spec().InputSchema), "format") {
		t.Fatalf("unexpected MCP tool adapter result: spec=%#v result=%#v err=%v", adapter.Spec(), result, err)
	}
	client.result = RemoteResult{Text: "tool failure", IsError: true}
	if _, err := adapter.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("MCP isError result was not converted to a tool failure")
	}
}

func contains(value, fragment string) bool {
	return len(fragment) == 0 || (len(value) >= len(fragment) && stringContains(value, fragment))
}

func stringContains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
