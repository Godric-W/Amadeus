package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ListResourcesTool struct {
	manager *Manager
	spec    tool.ToolSpec
}
type ReadResourceTool struct {
	manager  *Manager
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

func NewResourceTools(manager *Manager) (*ListResourcesTool, *ReadResourceTool, error) {
	if manager == nil {
		return nil, nil, errors.New("MCP resource tool manager is nil")
	}
	servers := manager.EnabledServers()
	if len(servers) == 0 {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(servers)
	if err != nil {
		return nil, nil, err
	}
	listSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s}},"required":["server"],"additionalProperties":false}`, encoded))
	readSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s},"uri":{"type":"string","minLength":1}},"required":["server","uri"],"additionalProperties":false}`, encoded))
	list := &ListResourcesTool{manager: manager, spec: tool.ToolSpec{Name: "mcp_list_resources", Description: "List untrusted resources exposed by one configured MCP server.", InputSchema: listSchema, SideEffect: tool.SideEffectNetwork, Idempotent: true}}
	read := &ReadResourceTool{manager: manager, maxBytes: defaultResultBytes, spec: tool.ToolSpec{Name: "mcp_read_resource", Description: "Read one previously discovered MCP resource. Text and image blobs are returned as bounded untrusted content.", InputSchema: readSchema, SideEffect: tool.SideEffectNetwork, Idempotent: true}}
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
	if err := validateSampleBinding(toolContext, value.manager); err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}
func (value *ListResourcesTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(listResourcesArguments)
	if !ok {
		return tool.ToolResult{}, errors.New("mcp_list_resources preparation state is invalid")
	}
	resources, err := value.manager.ListResources(toolContext.Context, arguments.Server)
	if err != nil {
		return tool.ToolResult{}, err
	}
	encoded, err := json.Marshal(struct {
		Server    string           `json:"server"`
		Resources []RemoteResource `json:"resources"`
	}{arguments.Server, resources})
	if err != nil {
		return tool.ToolResult{}, err
	}
	text, partial := boundText(string(encoded), defaultResultBytes)
	return tool.ToolResult{ToolName: "mcp_list_resources", Text: "Untrusted MCP resource catalog:\n" + text, Partial: partial, Data: resources, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: arguments.Server + " resources", Summary: fmt.Sprintf("%d resources", len(resources))}, Metadata: map[string]any{"server": arguments.Server, "resource_count": len(resources)}}, nil
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
	if err := validateSampleBinding(toolContext, value.manager); err != nil {
		return tool.PreparedToolUse{}, err
	}
	arguments.Server, arguments.URI = strings.TrimSpace(arguments.Server), strings.TrimSpace(arguments.URI)
	resources, err := value.manager.ListResources(toolContext.Context, arguments.Server)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	found := false
	for _, resource := range resources {
		if resource.URI == arguments.URI {
			found = true
			break
		}
	}
	if !found {
		return tool.PreparedToolUse{}, fmt.Errorf("MCP resource %q is not exposed by server %q", arguments.URI, arguments.Server)
	}
	key := "mcp-resource:" + arguments.Server + "/" + arguments.URI
	grant := policy.ExternalGrant(key)
	request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskModerate, policy.ApprovalCause{Kind: policy.ApprovalCauseExternalTool, Code: "mcp_resource", Detail: key})
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	request.PermissionKey = grant.Key
	request.Presentation = policy.MCPResourceApprovalPresentation(arguments.Server, arguments.URI)
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.PermissionEvaluation{Decision: tool.PermissionAsk, Request: &request, Grant: grant}}, nil
}
func (value *ReadResourceTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(readResourceArguments)
	if !ok {
		return tool.ToolResult{}, errors.New("mcp_read_resource preparation state is invalid")
	}
	contents, err := value.manager.ReadResource(toolContext.Context, arguments.Server, arguments.URI)
	if err != nil {
		return tool.ToolResult{}, err
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
				return tool.ToolResult{}, fmt.Errorf("decode MCP resource image %q: %w", content.URI, decodeErr)
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
