package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type fakeClient struct {
	tools             []RemoteTool
	resources         []RemoteResource
	contents          []RemoteResourceContent
	result            RemoteResult
	listErrs          []error
	callErrs          []error
	listCalls         int
	callCalls         int
	resourceListCalls int
	resourceReadCalls int
	closeCalls        int
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

func (client *fakeClient) ListResources(context.Context) ([]RemoteResource, error) {
	client.resourceListCalls++
	return append([]RemoteResource(nil), client.resources...), nil
}
func (client *fakeClient) ReadResource(context.Context, string) ([]RemoteResourceContent, error) {
	client.resourceReadCalls++
	return append([]RemoteResourceContent(nil), client.contents...), nil
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
	if _, err := manager.ListTools(context.Background(), "demo"); err != nil || factories != 2 || second.listCalls != 1 {
		t.Fatalf("MCP client was not reused: err=%v factories=%d client=%#v", err, factories, second)
	}
	if err := manager.Close(); err != nil || second.closeCalls != 1 {
		t.Fatalf("close manager: err=%v second=%#v", err, second)
	}
}

func TestToolAdapterSanitizesSchemaBoundsUntrustedResultAndUsesNetworkApprovalClass(t *testing.T) {
	client := &fakeClient{
		tools:  []RemoteTool{{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		result: RemoteResult{Text: "abcdefgh", IsError: false},
	}
	manager, err := NewManager(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewToolAdapter("demo", RemoteTool{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object","title":"ignored","properties":{"value":{"type":"string","format":"uri"}}}`)}, manager, AdapterOptions{MaxResultBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), adapter, json.RawMessage(`{"value":"ok"}`))
	if err != nil || adapter.Spec().Name != "mcp__demo__echo" || adapter.Spec().SideEffect != "network" || !result.Partial || !contains(result.Text, "Untrusted external MCP result") || contains(string(adapter.Spec().InputSchema), "title") || contains(string(adapter.Spec().InputSchema), "format") {
		t.Fatalf("unexpected MCP tool adapter result: spec=%#v result=%#v err=%v", adapter.Spec(), result, err)
	}
	client.result = RemoteResult{Text: "tool failure", IsError: true}
	if _, err := executePreparedTool(t, context.Background(), adapter, json.RawMessage(`{}`)); err == nil {
		t.Fatal("MCP isError result was not converted to a tool failure")
	}
}

func TestManagerCachesCatalogsAndValidatesResourceURI(t *testing.T) {
	client := &fakeClient{
		tools:     []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		resources: []RemoteResource{{URI: "fixture://one", Name: "One", MIMEType: "text/plain"}},
		contents:  []RemoteResourceContent{{URI: "fixture://one", MIMEType: "text/plain", Text: "hello"}},
	}
	manager, err := NewManager(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	firstTools, err := manager.ListTools(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	firstTools[0].Name = "mutated"
	secondTools, err := manager.ListTools(context.Background(), "demo")
	if err != nil || client.listCalls != 1 || secondTools[0].Name != "echo" {
		t.Fatalf("tool catalog cache failed: calls=%d tools=%#v err=%v", client.listCalls, secondTools, err)
	}
	if _, err := manager.ListResources(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ListResources(context.Background(), "demo"); err != nil || client.resourceListCalls != 1 {
		t.Fatalf("resource catalog cache failed: calls=%d err=%v", client.resourceListCalls, err)
	}
	contents, err := manager.ReadResource(context.Background(), "demo", "fixture://one")
	if err != nil || len(contents) != 1 || client.resourceReadCalls != 1 {
		t.Fatalf("read cached resource: contents=%#v calls=%d err=%v", contents, client.resourceReadCalls, err)
	}
	if _, err := manager.ReadResource(context.Background(), "demo", "fixture://missing"); err == nil || client.resourceReadCalls != 1 {
		t.Fatalf("unknown resource reached server: calls=%d err=%v", client.resourceReadCalls, err)
	}
}

func TestManagerBindingRevisionTracksCatalogAndReconnectRevalidatesTool(t *testing.T) {
	configured := Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}
	first := &fakeClient{
		tools:    []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		callErrs: []error{errors.New("connection lost")},
	}
	second := &fakeClient{tools: []RemoteTool{{Name: "different", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
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
	defer manager.Close()
	initial := manager.BindingSnapshot()
	if len(initial.Revision) != 64 || len(initial.Servers) != 1 || initial.Servers[0].ConnectionGeneration != 0 || initial.Servers[0].ToolsLoaded {
		t.Fatalf("unexpected initial MCP binding: %#v", initial)
	}
	if _, err := manager.ListTools(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	loaded := manager.BindingSnapshot()
	if loaded.Revision == initial.Revision || loaded.Servers[0].ConnectionGeneration != 1 || loaded.Servers[0].CatalogRevision == 0 || !loaded.Servers[0].ToolsLoaded {
		t.Fatalf("MCP binding did not track first catalog: %#v", loaded)
	}
	if _, err := manager.CallTool(context.Background(), "demo", "echo", json.RawMessage(`{}`)); err == nil {
		t.Fatal("MCP call was allowed after reconnect removed the Tool")
	}
	current := manager.BindingSnapshot()
	if current.Revision == loaded.Revision || current.Servers[0].ConnectionGeneration != 2 || second.callCalls != 0 {
		t.Fatalf("MCP reconnect did not revalidate catalog: binding=%#v second=%#v", current, second)
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
