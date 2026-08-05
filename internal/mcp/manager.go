package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Manager struct {
	configured Config
	factory    ClientFactory

	mutex     sync.Mutex
	clients   map[string]Client
	tools     map[string][]RemoteTool
	resources map[string][]RemoteResource
}

func NewManager(configured Config, factory ClientFactory) (*Manager, error) {
	if err := configured.Validate(); err != nil {
		return nil, err
	}
	if factory == nil {
		factory = NewClient
	}
	return &Manager{
		configured: configured, factory: factory,
		clients: make(map[string]Client), tools: make(map[string][]RemoteTool), resources: make(map[string][]RemoteResource),
	}, nil
}

func (manager *Manager) EnabledServers() []string {
	if manager == nil {
		return nil
	}
	return manager.configured.EnabledServers()
}

func (manager *Manager) ListTools(ctx context.Context, server string) ([]RemoteTool, error) {
	server = strings.TrimSpace(server)
	manager.mutex.Lock()
	cached, exists := manager.tools[server]
	manager.mutex.Unlock()
	if exists {
		return cloneRemoteTools(cached), nil
	}
	values, err := withRetry(manager, ctx, server, func(client Client) ([]RemoteTool, error) { return client.ListTools(ctx) })
	if err != nil {
		return nil, err
	}
	values = cloneRemoteTools(values)
	manager.mutex.Lock()
	manager.tools[server] = values
	manager.mutex.Unlock()
	return cloneRemoteTools(values), nil
}

func (manager *Manager) CallTool(ctx context.Context, server, name string, arguments json.RawMessage) (RemoteResult, error) {
	result, err := withRetry(manager, ctx, server, func(client Client) (RemoteResult, error) { return client.CallTool(ctx, name, arguments) })
	if err != nil {
		return RemoteResult{}, err
	}
	return result, nil
}

func (manager *Manager) ListResources(ctx context.Context, server string) ([]RemoteResource, error) {
	server = strings.TrimSpace(server)
	manager.mutex.Lock()
	cached, exists := manager.resources[server]
	manager.mutex.Unlock()
	if exists {
		return append([]RemoteResource(nil), cached...), nil
	}
	values, err := withRetry(manager, ctx, server, func(client Client) ([]RemoteResource, error) { return client.ListResources(ctx) })
	if err != nil {
		return nil, err
	}
	values = append([]RemoteResource(nil), values...)
	manager.mutex.Lock()
	manager.resources[server] = values
	manager.mutex.Unlock()
	return append([]RemoteResource(nil), values...), nil
}

func (manager *Manager) ReadResource(ctx context.Context, server, uri string) ([]RemoteResourceContent, error) {
	resources, err := manager.ListResources(ctx, server)
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
	return withRetry(manager, ctx, server, func(client Client) ([]RemoteResourceContent, error) { return client.ReadResource(ctx, uri) })
}

func (manager *Manager) ToolAdapters(ctx context.Context, server string, options AdapterOptions) ([]*ToolAdapter, error) {
	remote, err := manager.ListTools(ctx, server)
	if err != nil {
		return nil, err
	}
	tools := make([]*ToolAdapter, 0, len(remote))
	for _, value := range remote {
		adapter, err := NewToolAdapter(server, value, manager, options)
		if err != nil {
			return nil, err
		}
		tools = append(tools, adapter)
	}
	return tools, nil
}

func withRetry[T any](manager *Manager, ctx context.Context, server string, invoke func(Client) (T, error)) (T, error) {
	var zero T
	client, err := manager.client(ctx, server)
	if err != nil {
		return zero, err
	}
	value, err := invoke(client)
	if err == nil || ctx.Err() != nil {
		return value, err
	}
	manager.drop(server, client)
	client, reconnectErr := manager.client(ctx, server)
	if reconnectErr != nil {
		return zero, errors.Join(err, reconnectErr)
	}
	value, retryErr := invoke(client)
	if retryErr != nil {
		return zero, retryErr
	}
	return value, nil
}

func (manager *Manager) client(ctx context.Context, server string) (Client, error) {
	if manager == nil {
		return nil, errors.New("MCP manager is nil")
	}
	server = strings.TrimSpace(server)
	configured, ok := manager.configured.Server(server)
	if !ok {
		return nil, fmt.Errorf("MCP server %q is not configured", server)
	}
	if !configured.IsEnabled() {
		return nil, fmt.Errorf("MCP server %q is disabled", server)
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if client := manager.clients[server]; client != nil {
		return client, nil
	}
	client, err := manager.factory(ctx, configured)
	if err != nil {
		return nil, fmt.Errorf("start MCP server %q: %w", server, err)
	}
	if client == nil {
		return nil, fmt.Errorf("start MCP server %q: factory returned nil", server)
	}
	manager.clients[server] = client
	return client, nil
}

func (manager *Manager) drop(server string, client Client) {
	if manager == nil {
		return
	}
	manager.mutex.Lock()
	if manager.clients[server] == client {
		delete(manager.clients, server)
		delete(manager.tools, server)
		delete(manager.resources, server)
	}
	manager.mutex.Unlock()
	_ = client.Close()
}

func (manager *Manager) Close() error {
	if manager == nil {
		return nil
	}
	manager.mutex.Lock()
	clients := manager.clients
	manager.clients = make(map[string]Client)
	manager.tools = make(map[string][]RemoteTool)
	manager.resources = make(map[string][]RemoteResource)
	manager.mutex.Unlock()
	var closeErr error
	for _, client := range clients {
		if err := client.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

func cloneRemoteTools(values []RemoteTool) []RemoteTool {
	cloned := make([]RemoteTool, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].InputSchema = append(json.RawMessage(nil), value.InputSchema...)
	}
	return cloned
}
