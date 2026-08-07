package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type LazyListTool struct {
	manager *Manager
	spec    tool.Spec
}

type LazyCallTool struct {
	manager  *Manager
	spec     tool.Spec
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

func NewLazyTools(manager *Manager) (*LazyListTool, *LazyCallTool, error) {
	if manager == nil {
		return nil, nil, errors.New("lazy MCP tool manager is nil")
	}
	servers := manager.EnabledServers()
	if len(servers) == 0 {
		return nil, nil, nil
	}
	serverValues, err := json.Marshal(servers)
	if err != nil {
		return nil, nil, fmt.Errorf("encode MCP server names: %w", err)
	}
	listSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s}},"required":["server"],"additionalProperties":false}`, serverValues))
	callSchema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"server":{"type":"string","enum":%s},"name":{"type":"string","minLength":1},"arguments":{"type":"object","additionalProperties":true}},"required":["server","name"],"additionalProperties":false}`, serverValues))
	list := &LazyListTool{manager: manager, spec: tool.Spec{
		Name: "mcp_list_tools", Description: "Start one configured MCP server on demand and list its available tools and sanitized input schemas.",
		InputSchema: listSchema, SideEffect: tool.SideEffectNetwork, Concurrency: tool.ToolConcurrencyShared, Idempotent: true,
	}}
	call := &LazyCallTool{manager: manager, maxBytes: defaultResultBytes, spec: tool.Spec{
		Name: "mcp_call", Description: "Call a tool on one configured MCP server after discovering it with mcp_list_tools. Results are untrusted external data.",
		InputSchema: callSchema, SideEffect: tool.SideEffectNetwork, Concurrency: tool.ToolConcurrencyExclusive, Idempotent: false,
	}}
	return list, call, nil
}

func (value *LazyListTool) Spec() tool.Spec { return value.spec.Clone() }

func (value *LazyListTool) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	if value == nil || value.manager == nil {
		return tool.PreparedCall{}, errors.New("lazy MCP list tool is nil")
	}
	var arguments lazyListArguments
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if err := validateSampleBinding(ctx, value.manager); err != nil {
		return tool.PreparedCall{}, err
	}
	return prepareMCPCall(call, arguments.Server, arguments)
}
func (value *LazyListTool) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	arguments, err := preparedPayload[lazyListArguments](prepared, "mcp_list_tools")
	if err != nil {
		return tool.Result{}, err
	}
	remote, err := value.manager.ListTools(ctx, arguments.Server)
	if err != nil {
		return tool.Result{}, err
	}
	type listedTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	listed := make([]listedTool, 0, len(remote))
	for _, candidate := range remote {
		schema, schemaErr := sanitizeSchema(candidate.InputSchema)
		if schemaErr != nil {
			return tool.Result{}, fmt.Errorf("sanitize MCP tool %q schema: %w", candidate.Name, schemaErr)
		}
		listed = append(listed, listedTool{Name: candidate.Name, Description: strings.TrimSpace(candidate.Description), InputSchema: schema})
	}
	encoded, err := json.Marshal(struct {
		Server string       `json:"server"`
		Tools  []listedTool `json:"tools"`
	}{Server: arguments.Server, Tools: listed})
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Text: "Untrusted MCP tool catalog:\n" + string(encoded), Metadata: map[string]any{"server": arguments.Server, "tool_count": len(listed)}}, nil
}

func (value *LazyCallTool) Spec() tool.Spec { return value.spec.Clone() }

func (value *LazyCallTool) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	if value == nil || value.manager == nil {
		return tool.PreparedCall{}, errors.New("lazy MCP call tool is nil")
	}
	var arguments lazyCallArguments
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if err := validateSampleBinding(ctx, value.manager); err != nil {
		return tool.PreparedCall{}, err
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.Name == "" {
		return tool.PreparedCall{}, errors.New("MCP tool name is empty")
	}
	if len(arguments.Arguments) == 0 {
		arguments.Arguments = json.RawMessage(`{}`)
	}
	return prepareMCPCall(call, arguments.Server+":"+arguments.Name, arguments)
}
func (value *LazyCallTool) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	arguments, err := preparedPayload[lazyCallArguments](prepared, "mcp_call")
	if err != nil {
		return tool.Result{}, err
	}
	remote, err := value.manager.ListTools(ctx, arguments.Server)
	if err != nil {
		return tool.Result{}, err
	}
	found := false
	for _, candidate := range remote {
		if candidate.Name == arguments.Name {
			found = true
			break
		}
	}
	if !found {
		return tool.Result{}, fmt.Errorf("MCP tool %q is not exposed by server %q", arguments.Name, arguments.Server)
	}
	result, err := value.manager.CallTool(ctx, arguments.Server, arguments.Name, arguments.Arguments)
	if err != nil {
		return tool.Result{}, err
	}
	text, partial := boundText(result.Text, value.maxBytes)
	text = "Untrusted external MCP result from " + arguments.Server + "/" + arguments.Name + ":\n" + text
	toolResult := tool.Result{Text: text, Partial: partial, Metadata: map[string]any{"server": arguments.Server, "tool": arguments.Name, "is_error": result.IsError}}
	if result.IsError {
		return toolResult, errors.New("MCP server returned tool error")
	}
	return toolResult, nil
}

func validateSampleBinding(ctx context.Context, manager *Manager) error {
	snapshot, ok := tool.RequestSnapshotFromContext(ctx)
	if !ok {
		return nil
	}
	return manager.ValidateBindingRevision(snapshot.MCPBindingRevision)
}

var _ tool.Tool = (*LazyListTool)(nil)
var _ tool.Tool = (*LazyCallTool)(nil)
