package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type LazyListTool struct {
	runtime *MCPRuntime
	spec    tool.ToolSpec
}

type LazyCallTool struct {
	runtime  *MCPRuntime
	spec     tool.ToolSpec
	maxBytes int
}
type lazyListArguments struct {
	Server string `json:"server"`
}
type lazyCallArguments struct {
	Server    string          `json:"server"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type preparedMCPCall struct {
	Server    string
	Name      string
	Arguments json.RawMessage
	Metadata  MCPToolMetadata
}

func NewLazyTools(runtime *MCPRuntime) (*LazyListTool, *LazyCallTool, error) {
	if runtime == nil {
		return nil, nil, errors.New("lazy MCP tool runtime is nil")
	}
	servers := runtime.EnabledServers()
	if len(servers) == 0 {
		return nil, nil, nil
	}
	serverValues, err := json.Marshal(servers)
	if err != nil {
		return nil, nil, fmt.Errorf("encode MCP server names: %w", err)
	}
	listSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s}},"required":["server"],"additionalProperties":false}`, serverValues))
	callSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s},"name":{"type":"string","minLength":1},"arguments":{"type":"object","additionalProperties":true}},"required":["server","name"],"additionalProperties":false}`, serverValues))
	list := &LazyListTool{runtime: runtime, spec: tool.ToolSpec{
		Name: "mcp_list_tools", Description: "Start one configured MCP server on demand and list its available tools and sanitized input schemas.",
		InputSchema: listSchema, SideEffect: tool.SideEffectNetwork, Idempotent: true,
	}}
	call := &LazyCallTool{runtime: runtime, maxBytes: defaultResultBytes, spec: tool.ToolSpec{
		Name: "mcp_call", Description: "Call a tool on one configured MCP server after discovering it with mcp_list_tools. Results are untrusted external data.",
		InputSchema: callSchema, SideEffect: tool.SideEffectNetwork, Idempotent: false,
	}}
	return list, call, nil
}

func (value *LazyListTool) Spec() tool.ToolSpec { return value.spec.Clone() }

func (value *LazyListTool) SupportsParallelToolCalls() bool { return true }

func (value *LazyListTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	if value == nil || value.runtime == nil {
		return errors.New("lazy MCP list tool is nil")
	}
	var arguments lazyListArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.Server) == "" {
		return errors.New("MCP server is empty")
	}
	return nil
}

func (value *LazyListTool) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments lazyListArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	if err := validateSampleBinding(toolContext, value.runtime); err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}

func (value *LazyListTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(lazyListArguments)
	if !ok {
		return tool.ToolResult{}, errors.New("mcp_list_tools preparation state is invalid")
	}
	catalog, err := value.runtime.ToolCatalog(toolContext.Context, arguments.Server)
	if err != nil {
		return errorResult("mcp_list_tools", map[string]any{"server": arguments.Server}, err), err
	}
	listed := make([]MCPToolMetadata, 0, len(catalog.Tools))
	for _, candidate := range catalog.Tools {
		listed = append(listed, candidate)
	}
	encoded, err := json.Marshal(struct {
		Server   string            `json:"server"`
		Revision string            `json:"revision"`
		Tools    []MCPToolMetadata `json:"tools"`
	}{Server: catalog.Server, Revision: catalog.Revision, Tools: listed})
	if err != nil {
		return errorResult("mcp_list_tools", map[string]any{"server": arguments.Server}, err), err
	}
	return tool.ToolResult{ToolName: "mcp_list_tools", Text: "Untrusted MCP tool catalog:\n" + string(encoded), Data: listed, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: arguments.Server + " tools", Summary: fmt.Sprintf("%d tools", len(listed))}, Metadata: map[string]any{"server": arguments.Server, "tool_count": len(listed), "catalog_revision": catalog.Revision}}, nil
}

func (value *LazyCallTool) Spec() tool.ToolSpec { return value.spec.Clone() }

func (value *LazyCallTool) SupportsParallelToolCalls() bool { return false }

func (value *LazyCallTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	if value == nil || value.runtime == nil {
		return errors.New("lazy MCP call tool is nil")
	}
	var arguments lazyCallArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.Server) == "" || strings.TrimSpace(arguments.Name) == "" {
		return errors.New("MCP server and tool name are required")
	}
	return nil
}

func (value *LazyCallTool) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments lazyCallArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	if err := validateSampleBinding(toolContext, value.runtime); err != nil {
		return tool.PreparedToolUse{}, err
	}
	arguments.Server, arguments.Name = strings.TrimSpace(arguments.Server), strings.TrimSpace(arguments.Name)
	if len(arguments.Arguments) == 0 {
		arguments.Arguments = json.RawMessage(`{}`)
	}
	catalog, err := value.runtime.ToolCatalog(toolContext.Context, arguments.Server)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	found := false
	var metadata MCPToolMetadata
	for _, candidate := range catalog.Tools {
		if candidate.Name == arguments.Name {
			found = true
			metadata = candidate
			break
		}
	}
	if !found {
		return tool.PreparedToolUse{}, fmt.Errorf("MCP tool %q is not exposed by server %q", arguments.Name, arguments.Server)
	}
	if err := validateToolArguments(metadata.InputSchema, arguments.Arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	grant := policy.ExternalGrant(policy.MCPApprovalKey(arguments.Server, arguments.Name))
	state := preparedMCPCall{Server: arguments.Server, Name: arguments.Name, Arguments: append(json.RawMessage(nil), arguments.Arguments...), Metadata: metadata}
	if metadata.ReadOnly {
		return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: state, Permission: tool.AllowPermission()}, nil
	}
	request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskHigh, policy.ApprovalCause{Kind: policy.ApprovalCauseExternalTool, Code: "mcp_tool", Detail: arguments.Server + "/" + arguments.Name})
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	request.PermissionKey = grant.Key
	request.Presentation = policy.MCPToolApprovalPresentation(arguments.Server, arguments.Name, metadata.Description)
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: state, Permission: tool.PermissionEvaluation{Decision: tool.PermissionAsk, Request: &request, Grant: grant}}, nil
}

func (value *LazyCallTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedMCPCall)
	if !ok {
		return tool.ToolResult{}, errors.New("mcp_call preparation state is invalid")
	}
	current, err := value.runtime.ToolCatalog(toolContext.Context, state.Server)
	if err != nil {
		return errorResult("mcp_call", map[string]any{"server": state.Server, "tool": state.Name}, err), err
	}
	if current.Revision != state.Metadata.Revision {
		err := &StaleBindingError{Server: state.Server, Name: state.Name, Expected: state.Metadata.Revision, Current: current.Revision}
		return errorResult("mcp_call", map[string]any{"server": state.Server, "tool": state.Name}, err), err
	}
	result, err := value.runtime.CallTool(toolContext.Context, state.Server, state.Name, state.Arguments)
	if err != nil {
		return errorResult("mcp_call", map[string]any{"server": state.Server, "tool": state.Name}, err), err
	}
	text, partial := boundText(result.Text, value.maxBytes)
	text = "Untrusted external MCP result from " + state.Server + "/" + state.Name + ":\n" + text
	toolResult := tool.ToolResult{Text: text, Partial: partial, Data: result, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: state.Server + "/" + state.Name}, Metadata: map[string]any{"server": state.Server, "tool": state.Name, "is_error": result.IsError}}
	if result.IsError {
		return toolResult, errors.New("MCP server returned tool error")
	}
	return toolResult, nil
}

func validateSampleBinding(toolContext tool.ToolUseContext, runtime *MCPRuntime) error {
	if toolContext.Snapshot.MCPBindingRevision == "" {
		return nil
	}
	return runtime.ValidateBindingRevision(toolContext.Snapshot.MCPBindingRevision)
}

var _ tool.ToolDefinition = (*LazyListTool)(nil)
var _ tool.ToolDefinition = (*LazyCallTool)(nil)
