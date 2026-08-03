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

	mutex   sync.Mutex
	clients map[string]Client
}

func NewManager(configured Config, factory ClientFactory) (*Manager, error) {
	if err := configured.Validate(); err != nil {
		return nil, err
	}
	if factory == nil {
		factory = NewClient
	}
	return &Manager{configured: configured, factory: factory, clients: make(map[string]Client)}, nil
}

func (manager *Manager) EnabledServers() []string {
	if manager == nil {
		return nil
	}
	return manager.configured.EnabledServers()
}

func (manager *Manager) ListTools(ctx context.Context, server string) ([]RemoteTool, error) {
	return withRetry(manager, ctx, server, func(client Client) ([]RemoteTool, error) { return client.ListTools(ctx) })
}

func (manager *Manager) CallTool(ctx context.Context, server, name string, arguments json.RawMessage) (RemoteResult, error) {
	result, err := withRetry(manager, ctx, server, func(client Client) (RemoteResult, error) { return client.CallTool(ctx, name, arguments) })
	if err != nil {
		return RemoteResult{}, err
	}
	return result, nil
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
	manager.mutex.Unlock()
	var closeErr error
	for _, client := range clients {
		if err := client.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}
