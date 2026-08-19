package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type MCPRuntime struct {
	configured Config
	factory    ClientFactory
	revision   string

	mutex             sync.Mutex
	clients           map[string]Client
	tools             map[string][]RemoteTool
	resources         map[string][]RemoteResource
	generations       map[string]uint64
	toolRevisions     map[string]uint64
	resourceRevisions map[string]uint64
	servers           map[string]*serverState
	closed            bool
}

type serverState struct {
	mutex sync.Mutex
}

func NewMCPRuntime(configured Config, factory ClientFactory) (*MCPRuntime, error) {
	if err := configured.Validate(); err != nil {
		return nil, err
	}
	if factory == nil {
		factory = NewClient
	}
	encoded, err := json.Marshal(configured)
	if err != nil {
		return nil, fmt.Errorf("encode MCP configuration revision: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return &MCPRuntime{
		configured: configured, factory: factory,
		revision: hex.EncodeToString(digest[:]),
		clients:  make(map[string]Client), tools: make(map[string][]RemoteTool), resources: make(map[string][]RemoteResource),
		generations: make(map[string]uint64), toolRevisions: make(map[string]uint64), resourceRevisions: make(map[string]uint64),
		servers: make(map[string]*serverState),
	}, nil
}

func (runtime *MCPRuntime) Revision() string {
	if runtime == nil {
		return ""
	}
	binding := runtime.Binding().Revision
	digest := sha256.Sum256([]byte(runtime.revision + "\x00" + binding))
	return hex.EncodeToString(digest[:])
}

func (runtime *MCPRuntime) EnabledServers() []string {
	if runtime == nil {
		return nil
	}
	return runtime.configured.EnabledServers()
}

func (runtime *MCPRuntime) ListTools(ctx context.Context, server string) ([]RemoteTool, error) {
	server = strings.TrimSpace(server)
	state, err := runtime.serverState(server)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return runtime.listToolsLocked(ctx, server)
}

func (runtime *MCPRuntime) listToolsLocked(ctx context.Context, server string) ([]RemoteTool, error) {
	runtime.mutex.Lock()
	cached, exists := runtime.tools[server]
	runtime.mutex.Unlock()
	if exists {
		return cloneRemoteTools(cached), nil
	}
	values, err := withRetry(runtime, ctx, server, func(client Client) ([]RemoteTool, error) { return client.ListTools(ctx) })
	if err != nil {
		return nil, err
	}
	values = cloneRemoteTools(values)
	runtime.mutex.Lock()
	runtime.tools[server] = values
	runtime.toolRevisions[server]++
	runtime.mutex.Unlock()
	return cloneRemoteTools(values), nil
}

func (runtime *MCPRuntime) CallTool(ctx context.Context, server, name string, arguments json.RawMessage) (RemoteResult, error) {
	server = strings.TrimSpace(server)
	name = strings.TrimSpace(name)
	if err := runtime.requireTool(ctx, server, name); err != nil {
		return RemoteResult{}, err
	}
	state, err := runtime.serverState(server)
	if err != nil {
		return RemoteResult{}, err
	}
	state.mutex.Lock()
	client, err := runtime.client(ctx, server)
	if err != nil {
		state.mutex.Unlock()
		return RemoteResult{}, err
	}
	result, callErr := client.CallTool(ctx, name, arguments)
	if callErr == nil || ctx.Err() != nil {
		state.mutex.Unlock()
		return result, callErr
	}
	state.mutex.Unlock()
	runtime.drop(server, client)
	if validationErr := runtime.requireTool(ctx, server, name); validationErr != nil {
		return RemoteResult{}, errors.Join(callErr, fmt.Errorf("revalidate MCP tool after reconnect: %w", validationErr))
	}
	client, reconnectErr := runtime.client(ctx, server)
	if reconnectErr != nil {
		return RemoteResult{}, errors.Join(callErr, reconnectErr)
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return client.CallTool(ctx, name, arguments)
}

func (runtime *MCPRuntime) ListResources(ctx context.Context, server string) ([]RemoteResource, error) {
	server = strings.TrimSpace(server)
	state, err := runtime.serverState(server)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return runtime.listResourcesLocked(ctx, server)
}

func (runtime *MCPRuntime) listResourcesLocked(ctx context.Context, server string) ([]RemoteResource, error) {
	runtime.mutex.Lock()
	cached, exists := runtime.resources[server]
	runtime.mutex.Unlock()
	if exists {
		return append([]RemoteResource(nil), cached...), nil
	}
	values, err := withRetry(runtime, ctx, server, func(client Client) ([]RemoteResource, error) { return client.ListResources(ctx) })
	if err != nil {
		return nil, err
	}
	values = append([]RemoteResource(nil), values...)
	runtime.mutex.Lock()
	runtime.resources[server] = values
	runtime.resourceRevisions[server]++
	runtime.mutex.Unlock()
	return append([]RemoteResource(nil), values...), nil
}

func (runtime *MCPRuntime) ReadResource(ctx context.Context, server, uri string) ([]RemoteResourceContent, error) {
	server = strings.TrimSpace(server)
	uri = strings.TrimSpace(uri)
	resources, err := runtime.ListResources(ctx, server)
	if err != nil {
		return nil, err
	}
	found := false
	for _, resource := range resources {
		if resource.URI == uri {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("MCP resource %q is not exposed by server %q", uri, server)
	}
	state, err := runtime.serverState(server)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return withRetry(runtime, ctx, server, func(client Client) ([]RemoteResourceContent, error) { return client.ReadResource(ctx, uri) })
}

func (runtime *MCPRuntime) Refresh(ctx context.Context, server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return errors.New("MCP server name is empty")
	}
	state, err := runtime.serverState(server)
	if err != nil {
		return err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	runtime.mutex.Lock()
	client := runtime.clients[server]
	delete(runtime.clients, server)
	delete(runtime.tools, server)
	delete(runtime.resources, server)
	runtime.toolRevisions[server]++
	runtime.resourceRevisions[server]++
	runtime.mutex.Unlock()
	var closeErr error
	if client != nil {
		closeErr = client.Close()
	}
	_, toolsErr := runtime.listToolsLocked(ctx, server)
	_, resourcesErr := runtime.listResourcesLocked(ctx, server)
	return errors.Join(closeErr, toolsErr, resourcesErr)
}

func withRetry[T any](runtime *MCPRuntime, ctx context.Context, server string, invoke func(Client) (T, error)) (T, error) {
	var zero T
	client, err := runtime.client(ctx, server)
	if err != nil {
		return zero, err
	}
	value, err := invoke(client)
	if err == nil || ctx.Err() != nil {
		return value, err
	}
	runtime.drop(server, client)
	client, reconnectErr := runtime.client(ctx, server)
	if reconnectErr != nil {
		return zero, errors.Join(err, reconnectErr)
	}
	value, retryErr := invoke(client)
	if retryErr != nil {
		return zero, retryErr
	}
	return value, nil
}

func (runtime *MCPRuntime) client(ctx context.Context, server string) (Client, error) {
	if runtime == nil {
		return nil, errors.New("MCP runtime is nil")
	}
	server = strings.TrimSpace(server)
	configured, ok := runtime.configured.Server(server)
	if !ok {
		return nil, fmt.Errorf("MCP server %q is not configured", server)
	}
	if !configured.IsEnabled() {
		return nil, fmt.Errorf("MCP server %q is disabled", server)
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	if runtime.closed {
		return nil, errors.New("MCP runtime is closed")
	}
	if client := runtime.clients[server]; client != nil {
		return client, nil
	}
	client, err := runtime.factory(ctx, configured)
	if err != nil {
		return nil, fmt.Errorf("start MCP server %q: %w", server, err)
	}
	if client == nil {
		return nil, fmt.Errorf("start MCP server %q: factory returned nil", server)
	}
	runtime.clients[server] = client
	runtime.generations[server]++
	return client, nil
}

func (runtime *MCPRuntime) drop(server string, client Client) {
	if runtime == nil {
		return
	}
	runtime.mutex.Lock()
	if runtime.clients[server] == client {
		delete(runtime.clients, server)
		delete(runtime.tools, server)
		delete(runtime.resources, server)
		runtime.toolRevisions[server]++
		runtime.resourceRevisions[server]++
	}
	runtime.mutex.Unlock()
	_ = client.Close()
}

func (runtime *MCPRuntime) Close() error {
	if runtime == nil {
		return nil
	}
	servers := runtime.EnabledServers()
	states := make([]*serverState, 0, len(servers))
	for _, server := range servers {
		state, err := runtime.serverState(server)
		if err != nil {
			continue
		}
		state.mutex.Lock()
		states = append(states, state)
	}
	runtime.mutex.Lock()
	runtime.closed = true
	clients := runtime.clients
	runtime.clients = make(map[string]Client)
	runtime.tools = make(map[string][]RemoteTool)
	runtime.resources = make(map[string][]RemoteResource)
	runtime.mutex.Unlock()
	var closeErr error
	for _, client := range clients {
		if err := client.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	for index := len(states) - 1; index >= 0; index-- {
		states[index].mutex.Unlock()
	}
	return closeErr
}

func (runtime *MCPRuntime) Binding() MCPBinding {
	if runtime == nil {
		return MCPBinding{}
	}
	servers := runtime.EnabledServers()
	runtime.mutex.Lock()
	bindings := make([]MCPServerBinding, 0, len(servers))
	for _, server := range servers {
		_, toolsLoaded := runtime.tools[server]
		_, resourcesLoaded := runtime.resources[server]
		bindings = append(bindings, MCPServerBinding{
			Name: server, ConnectionGeneration: runtime.generations[server],
			ToolCatalogRevision: runtime.toolRevisions[server], ResourceCatalogRevision: runtime.resourceRevisions[server],
			Connected:   runtime.clients[server] != nil,
			ToolsLoaded: toolsLoaded, ResourcesLoaded: resourcesLoaded,
		})
	}
	runtime.mutex.Unlock()
	encoded, _ := json.Marshal(bindings)
	digest := sha256.Sum256(encoded)
	return MCPBinding{Revision: hex.EncodeToString(digest[:]), Servers: bindings}
}

func (runtime *MCPRuntime) ValidateBindingRevision(expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	current := runtime.Binding().Revision
	if current != expected {
		return &BindingRevisionError{Expected: expected, Current: current}
	}
	return nil
}

func (runtime *MCPRuntime) requireTool(ctx context.Context, server, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("MCP tool name is empty")
	}
	tools, err := runtime.ListTools(ctx, server)
	if err != nil {
		return err
	}
	for _, candidate := range tools {
		if candidate.Name == name {
			return nil
		}
	}
	return fmt.Errorf("MCP tool %q is not exposed by server %q", name, server)
}

func (runtime *MCPRuntime) serverState(server string) (*serverState, error) {
	if runtime == nil {
		return nil, errors.New("MCP runtime is nil")
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, errors.New("MCP server name is empty")
	}
	if _, ok := runtime.configured.Server(server); !ok {
		return nil, fmt.Errorf("MCP server %q is not configured", server)
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	state := runtime.servers[server]
	if state == nil {
		state = &serverState{}
		runtime.servers[server] = state
	}
	return state, nil
}

func cloneRemoteTools(values []RemoteTool) []RemoteTool {
	cloned := make([]RemoteTool, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].InputSchema = append(json.RawMessage(nil), value.InputSchema...)
	}
	return cloned
}
