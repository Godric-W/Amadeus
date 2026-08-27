package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type ListResourcesTool struct {
	runtime *MCPRuntime
	spec    tool.ToolSpec
}
type ReadResourceTool struct {
	runtime  *MCPRuntime
	spec     tool.ToolSpec
	maxBytes int
}
type listResourcesArguments struct {
	Server string `json:"server"`
}
type readResourceArguments struct {
	Server string `json:"server"`
	URI    string `json:"uri"`
}

type preparedResourceRead struct {
	Arguments readResourceArguments
	Revision  string
}

func NewResourceTools(runtime *MCPRuntime) (*ListResourcesTool, *ReadResourceTool, error) {
	if runtime == nil {
		return nil, nil, errors.New("MCP resource tool runtime is nil")
	}
	servers := runtime.EnabledServers()
	if len(servers) == 0 {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(servers)
	if err != nil {
		return nil, nil, err
	}
	listSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s,"description":"Configured MCP server whose resources should be listed."}},"required":["server"],"additionalProperties":false}`, encoded))
	readSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s,"description":"Configured MCP server exactly as used with mcp_list_resources."},"uri":{"type":"string","minLength":1,"description":"Exact resource URI returned by mcp_list_resources."}},"required":["server","uri"],"additionalProperties":false}`, encoded))
	list := &ListResourcesTool{runtime: runtime, spec: tool.ToolSpec{Name: "mcp_list_resources", Description: "List bounded untrusted resources exposed by one configured MCP server. Resources may provide files, schemas, or application context; inspect them instead of using web search when they are the authoritative source.", InputSchema: listSchema, SideEffect: tool.SideEffectNetwork, Idempotent: true}}
	read := &ReadResourceTool{runtime: runtime, maxBytes: defaultResultBytes, spec: tool.ToolSpec{Name: "mcp_read_resource", Description: "Read one resource URI previously discovered from the same MCP server. Text and image blobs are returned as bounded untrusted content.", InputSchema: readSchema, SideEffect: tool.SideEffectNetwork, Idempotent: true}}
	return list, read, nil
}

func (value *ListResourcesTool) Spec() tool.ToolSpec             { return value.spec.Clone() }
func (value *ListResourcesTool) SupportsParallelToolCalls() bool { return true }
func (value *ListResourcesTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments listResourcesArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.Server) == "" {
		return errors.New("MCP server is empty")
	}
	return nil
}
func (value *ListResourcesTool) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments listResourcesArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	if err := validateSampleBinding(toolContext, value.runtime); err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}
func (value *ListResourcesTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(listResourcesArguments)
	if !ok {
		return tool.ToolResult{}, errors.New("mcp_list_resources preparation state is invalid")
	}
	catalog, err := value.runtime.ResourceCatalog(toolContext.Context, arguments.Server)
	if err != nil {
		return errorResult("mcp_list_resources", map[string]any{"server": arguments.Server}, err), err
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return errorResult("mcp_list_resources", map[string]any{"server": arguments.Server}, err), err
	}
	text, partial := boundText(string(encoded), defaultResultBytes)
	return tool.ToolResult{ToolName: "mcp_list_resources", Text: "Untrusted MCP resource catalog:\n" + text, Partial: partial, Data: catalog.Resources, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: arguments.Server + " resources", Summary: fmt.Sprintf("%d resources", len(catalog.Resources))}, Metadata: map[string]any{"server": arguments.Server, "resource_count": len(catalog.Resources), "catalog_revision": catalog.Revision}}, nil
}
func (value *ReadResourceTool) Spec() tool.ToolSpec             { return value.spec.Clone() }
func (value *ReadResourceTool) SupportsParallelToolCalls() bool { return true }
func (value *ReadResourceTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments readResourceArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.Server) == "" || strings.TrimSpace(arguments.URI) == "" {
		return errors.New("MCP server and resource URI are required")
	}
	return nil
}
func (value *ReadResourceTool) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments readResourceArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	if err := validateSampleBinding(toolContext, value.runtime); err != nil {
		return tool.PreparedToolUse{}, err
	}
	arguments.Server, arguments.URI = strings.TrimSpace(arguments.Server), strings.TrimSpace(arguments.URI)
	catalog, err := value.runtime.ResourceCatalog(toolContext.Context, arguments.Server)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	found := false
	for _, resource := range catalog.Resources {
		if resource.URI == arguments.URI {
			found = true
			break
		}
	}
	if !found {
		return tool.PreparedToolUse{}, fmt.Errorf("MCP resource %q is not exposed by server %q", arguments.URI, arguments.Server)
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: preparedResourceRead{Arguments: arguments, Revision: catalog.Revision}, Permission: tool.AllowPermission()}, nil
}
func (value *ReadResourceTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedResourceRead)
	if !ok {
		return tool.ToolResult{}, errors.New("mcp_read_resource preparation state is invalid")
	}
	current, err := value.runtime.ResourceCatalog(toolContext.Context, state.Arguments.Server)
	if err != nil {
		return errorResult("mcp_read_resource", map[string]any{"server": state.Arguments.Server, "uri": state.Arguments.URI}, err), err
	}
	if current.Revision != state.Revision {
		err := &StaleBindingError{Server: state.Arguments.Server, Resource: state.Arguments.URI, Expected: state.Revision, Current: current.Revision}
		return errorResult("mcp_read_resource", map[string]any{"server": state.Arguments.Server, "uri": state.Arguments.URI}, err), err
	}
	arguments := state.Arguments
	contents, err := value.runtime.ReadResource(toolContext.Context, arguments.Server, arguments.URI)
	if err != nil {
		return errorResult("mcp_read_resource", map[string]any{"server": arguments.Server, "uri": arguments.URI}, err), err
	}
	var text strings.Builder
	parts := make([]tool.ContentPart, 0)
	partial := false
	for _, content := range contents {
		if content.Text != "" {
			text.WriteString(content.Text)
			if !strings.HasSuffix(content.Text, "\n") {
				text.WriteByte('\n')
			}
		}
		if content.Blob != "" && supportedMCPImageType(content.MIMEType) {
			if base64.StdEncoding.DecodedLen(len(content.Blob)) > value.maxBytes {
				partial = true
				continue
			}
			decoded, decodeErr := base64.StdEncoding.DecodeString(content.Blob)
			if decodeErr != nil {
				err := fmt.Errorf("decode MCP resource image %q: %w", content.URI, decodeErr)
				return errorResult("mcp_read_resource", map[string]any{"server": arguments.Server, "uri": arguments.URI}, err), err
			}
			if len(decoded) > value.maxBytes {
				partial = true
				continue
			}
			parts = append(parts, tool.ContentPart{Kind: tool.ContentImage, MediaType: content.MIMEType, Data: content.Blob})
		}
	}
	bounded, truncated := boundText(text.String(), value.maxBytes)
	partial = partial || truncated
	return tool.ToolResult{ToolName: "mcp_read_resource", Text: "Untrusted MCP resource from " + arguments.Server + ":\n" + bounded, Parts: parts, Partial: partial, Data: contents, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: arguments.URI, Summary: arguments.Server}, Metadata: map[string]any{"server": arguments.Server, "uri": arguments.URI, "content_count": len(contents)}}, nil
}

func supportedMCPImageType(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

var _ tool.ToolDefinition = (*ListResourcesTool)(nil)
var _ tool.ToolDefinition = (*ReadResourceTool)(nil)
