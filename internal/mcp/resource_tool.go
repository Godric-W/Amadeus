package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type ListResourcesTool struct {
	manager *Manager
	spec    tool.Spec
}
type ReadResourceTool struct {
	manager  *Manager
	spec     tool.Spec
	maxBytes int
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
	list := &ListResourcesTool{manager: manager, spec: tool.Spec{Name: "mcp_list_resources", Description: "List untrusted resources exposed by one configured MCP server.", InputSchema: listSchema, SideEffect: tool.SideEffectNetwork, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}}}
	read := &ReadResourceTool{manager: manager, maxBytes: defaultResultBytes, spec: tool.Spec{Name: "mcp_read_resource", Description: "Read one previously discovered MCP resource. Text and image blobs are returned as bounded untrusted content.", InputSchema: readSchema, SideEffect: tool.SideEffectNetwork, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}}}
	return list, read, nil
}

func (value *ListResourcesTool) Spec() tool.Spec { return value.spec.Clone() }
func (value *ListResourcesTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	resources, err := value.manager.ListResources(ctx, arguments.Server)
	if err != nil {
		return tool.Result{}, err
	}
	encoded, err := json.Marshal(struct {
		Server    string           `json:"server"`
		Resources []RemoteResource `json:"resources"`
	}{arguments.Server, resources})
	if err != nil {
		return tool.Result{}, err
	}
	text, partial := boundText(string(encoded), defaultResultBytes)
	return tool.Result{ToolName: "mcp_list_resources", Text: "Untrusted MCP resource catalog:\n" + text, Partial: partial, Metadata: map[string]any{"server": arguments.Server, "resource_count": len(resources)}}, nil
}
func (value *ReadResourceTool) Spec() tool.Spec { return value.spec.Clone() }
func (value *ReadResourceTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments struct {
		Server string `json:"server"`
		URI    string `json:"uri"`
	}
	if err := json.Unmarshal(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	arguments.URI = strings.TrimSpace(arguments.URI)
	if arguments.URI == "" {
		return tool.Result{}, errors.New("MCP resource URI is empty")
	}
	contents, err := value.manager.ReadResource(ctx, arguments.Server, arguments.URI)
	if err != nil {
		return tool.Result{}, err
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
				return tool.Result{}, fmt.Errorf("decode MCP resource image %q: %w", content.URI, decodeErr)
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
	return tool.Result{ToolName: "mcp_read_resource", Text: "Untrusted MCP resource from " + arguments.Server + ":\n" + bounded, Parts: parts, Partial: partial, Metadata: map[string]any{"server": arguments.Server, "uri": arguments.URI, "content_count": len(contents)}}, nil
}

func supportedMCPImageType(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

var _ tool.Tool = (*ListResourcesTool)(nil)
var _ tool.Tool = (*ReadResourceTool)(nil)
