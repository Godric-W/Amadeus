package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (runtime *MCPRuntime) ToolCatalog(ctx context.Context, server string) (ToolCatalog, error) {
	server = strings.TrimSpace(server)
	tools, err := runtime.ListTools(ctx, server)
	if err != nil {
		return ToolCatalog{}, err
	}
	runtime.mutex.Lock()
	revision := runtime.toolRevisions[server]
	runtime.mutex.Unlock()
	metadata := make([]MCPToolMetadata, 0, len(tools))
	for _, value := range tools {
		schema, schemaErr := sanitizeSchema(value.InputSchema)
		if schemaErr != nil {
			return ToolCatalog{}, fmt.Errorf("sanitize MCP tool %q schema: %w", value.Name, schemaErr)
		}
		metadata = append(metadata, MCPToolMetadata{
			Server: server, Name: value.Name, Description: value.Description,
			InputSchema: schema, ReadOnly: value.ReadOnlyHint,
			Idempotent: value.IdempotentHint, SupportsParallelCalls: value.SupportsParallelCalls,
			Revision: fmt.Sprintf("%d", revision),
		})
	}
	sort.Slice(metadata, func(left, right int) bool { return metadata[left].Name < metadata[right].Name })
	return ToolCatalog{Server: server, Revision: fmt.Sprintf("%d", revision), Tools: cloneToolMetadata(metadata)}, nil
}

func (runtime *MCPRuntime) ResourceCatalog(ctx context.Context, server string) (ResourceCatalog, error) {
	server = strings.TrimSpace(server)
	resources, err := runtime.ListResources(ctx, server)
	if err != nil {
		return ResourceCatalog{}, err
	}
	runtime.mutex.Lock()
	revision := runtime.resourceRevisions[server]
	runtime.mutex.Unlock()
	metadata := make([]MCPResourceMetadata, 0, len(resources))
	for _, value := range resources {
		metadata = append(metadata, MCPResourceMetadata{
			Server: server, URI: value.URI, Name: value.Name, Description: value.Description,
			MIMEType: value.MIMEType, Revision: fmt.Sprintf("%d", revision),
		})
	}
	sort.Slice(metadata, func(left, right int) bool { return metadata[left].URI < metadata[right].URI })
	return ResourceCatalog{Server: server, Revision: fmt.Sprintf("%d", revision), Resources: cloneResourceMetadata(metadata)}, nil
}
