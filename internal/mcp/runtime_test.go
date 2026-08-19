package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestRuntimeIsLazyReusesClientAndReconnectsOnce(t *testing.T) {
	configured := Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}
	first := &fakeClient{listErrs: []error{errors.New("connection lost")}}
	second := &fakeClient{tools: []RemoteTool{{Name: "echo", Description: "Echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	factories := 0
	manager, err := NewMCPRuntime(configured, func(context.Context, ServerConfig) (Client, error) {
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

func TestRuntimeCachesCatalogsAndValidatesResourceURI(t *testing.T) {
	client := &fakeClient{
		tools:     []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		resources: []RemoteResource{{URI: "fixture://one", Name: "One", MIMEType: "text/plain"}},
		contents:  []RemoteResourceContent{{URI: "fixture://one", MIMEType: "text/plain", Text: "hello"}},
	}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
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

func TestRuntimeBindingRevisionTracksCatalogAndReconnectRevalidatesTool(t *testing.T) {
	configured := Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}
	first := &fakeClient{
		tools:    []RemoteTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		callErrs: []error{errors.New("connection lost")},
	}
	second := &fakeClient{tools: []RemoteTool{{Name: "different", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	factories := 0
	manager, err := NewMCPRuntime(configured, func(context.Context, ServerConfig) (Client, error) {
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
	initial := manager.Binding()
	if len(initial.Revision) != 64 || len(initial.Servers) != 1 || initial.Servers[0].ConnectionGeneration != 0 || initial.Servers[0].ToolsLoaded {
		t.Fatalf("unexpected initial MCP binding: %#v", initial)
	}
	if _, err := manager.ListTools(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	loaded := manager.Binding()
	if loaded.Revision == initial.Revision || loaded.Servers[0].ConnectionGeneration != 1 || loaded.Servers[0].ToolCatalogRevision == 0 || !loaded.Servers[0].ToolsLoaded || !loaded.Servers[0].Connected {
		t.Fatalf("MCP binding did not track first catalog: %#v", loaded)
	}
	if _, err := manager.CallTool(context.Background(), "demo", "echo", json.RawMessage(`{}`)); err == nil {
		t.Fatal("MCP call was allowed after reconnect removed the Tool")
	}
	current := manager.Binding()
	if current.Revision == loaded.Revision || current.Servers[0].ConnectionGeneration != 2 || second.callCalls != 0 {
		t.Fatalf("MCP reconnect did not revalidate catalog: binding=%#v second=%#v", current, second)
	}
}

func TestRuntimeRefreshRestartsServerAndInvalidatesBothCatalogs(t *testing.T) {
	first := &fakeClient{tools: []RemoteTool{{Name: "old", InputSchema: json.RawMessage(`{"type":"object"}`)}}, resources: []RemoteResource{{URI: "fixture://old"}}}
	second := &fakeClient{tools: []RemoteTool{{Name: "new", InputSchema: json.RawMessage(`{"type":"object"}`)}}, resources: []RemoteResource{{URI: "fixture://new"}}}
	created := 0
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) {
		created++
		if created == 1 {
			return first, nil
		}
		return second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.ToolCatalog(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResourceCatalog(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Refresh(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	catalog, err := manager.ToolCatalog(context.Background(), "demo")
	if err != nil || len(catalog.Tools) != 1 || catalog.Tools[0].Name != "new" || first.closeCalls != 1 || created != 2 {
		t.Fatalf("refresh did not replace MCP catalogs: catalog=%#v first=%#v created=%d err=%v", catalog, first, created, err)
	}
	resources, err := manager.ResourceCatalog(context.Background(), "demo")
	if err != nil || len(resources.Resources) != 1 || resources.Resources[0].URI != "fixture://new" {
		t.Fatalf("refresh did not replace resource catalog: catalog=%#v err=%v", resources, err)
	}
}

func TestRuntimeKeepsToolAndResourceRevisionsIndependent(t *testing.T) {
	client := &fakeClient{
		tools:     []RemoteTool{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		resources: []RemoteResource{{URI: "fixture://one"}},
	}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	tools, err := manager.ToolCatalog(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResourceCatalog(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	toolsAfterResource, err := manager.ToolCatalog(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if tools.Revision != toolsAfterResource.Revision {
		t.Fatalf("resource discovery invalidated tool catalog: before=%s after=%s", tools.Revision, toolsAfterResource.Revision)
	}
}

func TestRuntimeSerializesConcurrentDiscoveryPerServer(t *testing.T) {
	client := &serializedDiscoveryClient{}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := manager.ListTools(context.Background(), "demo"); err != nil {
				t.Errorf("concurrent tool discovery: %v", err)
			}
		}()
	}
	wait.Wait()
	if calls := client.listCalls.Load(); calls != 1 || client.maxActive.Load() != 1 {
		t.Fatalf("MCP discovery was not serialized: calls=%d max_active=%d", calls, client.maxActive.Load())
	}
}

func TestRuntimeRejectsOperationsAfterShutdown(t *testing.T) {
	client := &fakeClient{tools: []RemoteTool{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ListTools(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil || client.closeCalls != 1 {
		t.Fatalf("MCP shutdown failed: err=%v close_calls=%d", err, client.closeCalls)
	}
	if _, err := manager.ListTools(context.Background(), "demo"); err == nil || !contains(err.Error(), "closed") {
		t.Fatalf("closed MCP runtime accepted operation: %v", err)
	}
}

type serializedDiscoveryClient struct {
	active    atomic.Int32
	maxActive atomic.Int32
	listCalls atomic.Int32
}

func (client *serializedDiscoveryClient) ListTools(context.Context) ([]RemoteTool, error) {
	client.listCalls.Add(1)
	active := client.active.Add(1)
	for {
		maximum := client.maxActive.Load()
		if active <= maximum || client.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	time.Sleep(2 * time.Millisecond)
	client.active.Add(-1)
	return []RemoteTool{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}

func (client *serializedDiscoveryClient) ListResources(context.Context) ([]RemoteResource, error) {
	return nil, nil
}
func (client *serializedDiscoveryClient) CallTool(context.Context, string, json.RawMessage) (RemoteResult, error) {
	return RemoteResult{}, nil
}
func (client *serializedDiscoveryClient) ReadResource(context.Context, string) ([]RemoteResourceContent, error) {
	return nil, nil
}
func (client *serializedDiscoveryClient) Close() error { return nil }

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
