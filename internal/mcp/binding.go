package mcp

import "encoding/json"

type MCPServerBinding struct {
	Name                    string `json:"name"`
	ConnectionGeneration    uint64 `json:"connection_generation"`
	ToolCatalogRevision     uint64 `json:"tool_catalog_revision"`
	ResourceCatalogRevision uint64 `json:"resource_catalog_revision"`
	Connected               bool   `json:"connected"`
	ToolsLoaded             bool   `json:"tools_loaded"`
	ResourcesLoaded         bool   `json:"resources_loaded"`
}

type MCPBinding struct {
	Revision string             `json:"revision"`
	Servers  []MCPServerBinding `json:"servers"`
}

type MCPToolMetadata struct {
	Server                string          `json:"server"`
	Name                  string          `json:"name"`
	Description           string          `json:"description"`
	InputSchema           json.RawMessage `json:"input_schema"`
	ReadOnly              bool            `json:"read_only"`
	Idempotent            bool            `json:"idempotent"`
	SupportsParallelCalls bool            `json:"supports_parallel_calls"`
	Revision              string          `json:"revision"`
}

type MCPResourceMetadata struct {
	Server      string `json:"server"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type"`
	Revision    string `json:"revision"`
}

type ToolCatalog struct {
	Server   string            `json:"server"`
	Revision string            `json:"revision"`
	Tools    []MCPToolMetadata `json:"tools"`
}

type ResourceCatalog struct {
	Server    string                `json:"server"`
	Revision  string                `json:"revision"`
	Resources []MCPResourceMetadata `json:"resources"`
}

func cloneToolMetadata(values []MCPToolMetadata) []MCPToolMetadata {
	cloned := make([]MCPToolMetadata, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].InputSchema = append(json.RawMessage(nil), value.InputSchema...)
	}
	return cloned
}

func cloneResourceMetadata(values []MCPResourceMetadata) []MCPResourceMetadata {
	return append([]MCPResourceMetadata(nil), values...)
}
