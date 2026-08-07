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

type ServerBinding struct {
	Name                 string `json:"name"`
	ConnectionGeneration uint64 `json:"connection_generation"`
	CatalogRevision      uint64 `json:"catalog_revision"`
	ToolsLoaded          bool   `json:"tools_loaded"`
	ResourcesLoaded      bool   `json:"resources_loaded"`
}

type BindingSnapshot struct {
	Revision string          `json:"revision"`
	Servers  []ServerBinding `json:"servers"`
}

type Manager struct {
	configured Config
	factory    ClientFactory

	mutex            sync.Mutex
	clients          map[string]Client
	tools            map[string][]RemoteTool
	resources        map[string][]RemoteResource
	generations      map[string]uint64
	catalogRevisions map[string]uint64
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
		generations: make(map[string]uint64), catalogRevisions: make(map[string]uint64),
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
	manager.catalogRevisions[server]++
	manager.mutex.Unlock()
	return cloneRemoteTools(values), nil
}

func (manager *Manager) CallTool(ctx context.Context, server, name string, arguments json.RawMessage) (RemoteResult, error) {
	if err := manager.requireTool(ctx, server, name); err != nil {
		return RemoteResult{}, err
	}
	client, err := manager.client(ctx, server)
	if err != nil {
		return RemoteResult{}, err
	}
	result, callErr := client.CallTool(ctx, name, arguments)
	if callErr == nil || ctx.Err() != nil {
		return result, callErr
	}
	manager.drop(server, client)
	if validationErr := manager.requireTool(ctx, server, name); validationErr != nil {
		return RemoteResult{}, errors.Join(callErr, fmt.Errorf("revalidate MCP tool after reconnect: %w", validationErr))
	}
	client, reconnectErr := manager.client(ctx, server)
	if reconnectErr != nil {
		return RemoteResult{}, errors.Join(callErr, reconnectErr)
	}
	return client.CallTool(ctx, name, arguments)
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
	manager.catalogRevisions[server]++
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
	manager.generations[server]++
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
		manager.catalogRevisions[server]++
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

func (manager *Manager) BindingSnapshot() BindingSnapshot {
	if manager == nil {
		return BindingSnapshot{}
	}
	servers := manager.EnabledServers()
	manager.mutex.Lock()
	bindings := make([]ServerBinding, 0, len(servers))
	for _, server := range servers {
		_, toolsLoaded := manager.tools[server]
		_, resourcesLoaded := manager.resources[server]
		bindings = append(bindings, ServerBinding{
			Name: server, ConnectionGeneration: manager.generations[server], CatalogRevision: manager.catalogRevisions[server],
			ToolsLoaded: toolsLoaded, ResourcesLoaded: resourcesLoaded,
		})
	}
	manager.mutex.Unlock()
	encoded, _ := json.Marshal(bindings)
	digest := sha256.Sum256(encoded)
	return BindingSnapshot{Revision: hex.EncodeToString(digest[:]), Servers: bindings}
}

func (manager *Manager) ValidateBindingRevision(expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	current := manager.BindingSnapshot().Revision
	if current != expected {
		return fmt.Errorf("MCP binding changed since model sampling: expected %s, current %s", expected, current)
	}
	return nil
}

func (manager *Manager) requireTool(ctx context.Context, server, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("MCP tool name is empty")
	}
	tools, err := manager.ListTools(ctx, server)
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

func cloneRemoteTools(values []RemoteTool) []RemoteTool {
	cloned := make([]RemoteTool, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].InputSchema = append(json.RawMessage(nil), value.InputSchema...)
	}
	return cloned
}
