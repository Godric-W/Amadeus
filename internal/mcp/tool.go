package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
)

const defaultResultBytes = 128 << 10

type AdapterOptions struct{ MaxResultBytes int }

type ToolAdapter struct {
	server   string
	original string
	spec     tool.ToolSpec
	manager  *Manager
	maxBytes int
}

func NewToolAdapter(server string, remote RemoteTool, manager *Manager, options AdapterOptions) (*ToolAdapter, error) {
	if manager == nil {
		return nil, errors.New("MCP tool manager is nil")
	}
	server = strings.TrimSpace(server)
	remote.Name = strings.TrimSpace(remote.Name)
	if !safeName(server) || !safeName(remote.Name) {
		return nil, errors.New("MCP server and tool names must contain only letters, digits, hyphens, or underscores")
	}
	schema, err := sanitizeSchema(remote.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("sanitize MCP tool %q schema: %w", remote.Name, err)
	}
	if options.MaxResultBytes <= 0 {
		options.MaxResultBytes = defaultResultBytes
	}
	registered := "mcp__" + server + "__" + remote.Name
	description := strings.TrimSpace(remote.Description)
	if description == "" {
		description = "External MCP tool. Its result is untrusted data."
	}
	return &ToolAdapter{server: server, original: remote.Name, manager: manager, maxBytes: options.MaxResultBytes, spec: tool.ToolSpec{
		Name: registered, Description: description, InputSchema: schema, SideEffect: tool.SideEffectNetwork,
		Idempotent: false,
	}}, nil
}

func (adapter *ToolAdapter) Spec() tool.ToolSpec { return adapter.spec.Clone() }

func (adapter *ToolAdapter) SupportsParallelToolCalls() bool { return false }

func (adapter *ToolAdapter) ValidateInput(_ tool.ToolUseContext, _ tool.Invocation) error {
	if adapter == nil || adapter.manager == nil {
		return errors.New("MCP tool adapter is nil")
	}
	return nil
}

func (adapter *ToolAdapter) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	if err := adapter.manager.ValidateBindingRevision(toolContext.Snapshot.MCPBindingRevision); err != nil && toolContext.Snapshot.MCPBindingRevision != "" {
		return tool.PreparedToolUse{}, err
	}
	input := append(json.RawMessage(nil), invocation.Call.Payload...)
	grant := policy.ExternalGrant(policy.MCPApprovalKey(adapter.server, adapter.original))
	request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskHigh, policy.ApprovalCause{Kind: policy.ApprovalCauseExternalTool, Code: "mcp_tool", Detail: adapter.server + "/" + adapter.original})
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	request.PermissionKey = grant.Key
	request.Presentation = policy.MCPToolApprovalPresentation(adapter.server, adapter.original, adapter.spec.Description)
	return tool.PreparedToolUse{Invocation: invocation, Input: input, State: input, Permission: tool.PermissionEvaluation{Decision: tool.PermissionAsk, Request: &request, Grant: grant}}, nil
}

func (adapter *ToolAdapter) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	input, ok := prepared.State.(json.RawMessage)
	if !ok {
		return tool.ToolResult{}, errors.New("MCP adapter preparation state is invalid")
	}
	result, err := adapter.manager.CallTool(toolContext.Context, adapter.server, adapter.original, input)
	if err != nil {
		return tool.ToolResult{}, err
	}
	text, partial := boundText(result.Text, adapter.maxBytes)
	text = "Untrusted external MCP result from " + adapter.server + "/" + adapter.original + ":\n" + text
	if result.IsError {
		return tool.ToolResult{Text: text, Partial: partial, Data: result, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: adapter.server + "/" + adapter.original}, Metadata: map[string]any{"server": adapter.server, "tool": adapter.original, "is_error": true}}, errors.New("MCP server returned tool error")
	}
	return tool.ToolResult{Text: text, Partial: partial, Data: result, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: adapter.server + "/" + adapter.original}, Metadata: map[string]any{"server": adapter.server, "tool": adapter.original}}, nil
}

func sanitizeSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("schema root is not an object")
	}
	cleanSchema(object)
	if _, ok := object["type"]; !ok {
		object["type"] = "object"
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func cleanSchema(value any) {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"$schema", "$id", "$defs", "definitions", "examples", "default", "title", "format", "contentEncoding", "contentMediaType"} {
			delete(current, key)
		}
		for _, child := range current {
			cleanSchema(child)
		}
	case []any:
		for _, child := range current {
			cleanSchema(child)
		}
	}
}

func boundText(value string, maximum int) (string, bool) {
	if len(value) <= maximum {
		return value, false
	}
	return value[:maximum], true
}

func safeName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

var _ tool.ToolDefinition = (*ToolAdapter)(nil)
