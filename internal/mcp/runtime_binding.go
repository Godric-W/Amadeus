package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

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
