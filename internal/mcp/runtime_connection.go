package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type serverState struct {
	mutex sync.Mutex
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
