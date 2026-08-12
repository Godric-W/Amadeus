package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type LazyListTool struct {
	manager *Manager
	spec    tool.Spec
}

type LazyCallTool struct {
	manager   *Manager
	spec      tool.Spec
	maxBytes  int
	approvals policy.ApprovalHandler
	rules     *policy.SessionRuleStore
	events    event.Sink
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
	return NewLazyToolsWithApproval(manager, LazyApprovalOptions{})
}

type LazyApprovalOptions struct {
	Approvals policy.ApprovalHandler
	Rules     *policy.SessionRuleStore
	Events    event.Sink
}

func NewLazyToolsWithApproval(manager *Manager, options LazyApprovalOptions) (*LazyListTool, *LazyCallTool, error) {
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
		InputSchema: listSchema, SideEffect: tool.SideEffectNetwork, Idempotent: true,
	}}
	if options.Rules == nil {
		options.Rules = policy.NewSessionRuleStore()
	}
	call := &LazyCallTool{manager: manager, maxBytes: defaultResultBytes, approvals: options.Approvals, rules: options.Rules, events: options.Events, spec: tool.Spec{
		Name: "mcp_call", Description: "Call a tool on one configured MCP server after discovering it with mcp_list_tools. Results are untrusted external data.",
		InputSchema: callSchema, SideEffect: tool.SideEffectNetwork, Idempotent: false,
	}}
	return list, call, nil
}

func (value *LazyListTool) Spec() tool.Spec { return value.spec.Clone() }

func (value *LazyListTool) SupportsParallelToolCalls() bool { return true }

func (value *LazyListTool) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	if value == nil || value.manager == nil {
		return tool.Output{}, errors.New("lazy MCP list tool is nil")
	}
	var arguments lazyListArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if err := validateSampleBinding(ctx, value.manager); err != nil {
		return tool.Output{}, err
	}
	remote, err := value.manager.ListTools(ctx, arguments.Server)
	if err != nil {
		return tool.Output{}, err
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
			return tool.Output{}, fmt.Errorf("sanitize MCP tool %q schema: %w", candidate.Name, schemaErr)
		}
		listed = append(listed, listedTool{Name: candidate.Name, Description: strings.TrimSpace(candidate.Description), InputSchema: schema})
	}
	encoded, err := json.Marshal(struct {
		Server string       `json:"server"`
		Tools  []listedTool `json:"tools"`
	}{Server: arguments.Server, Tools: listed})
	if err != nil {
		return tool.Output{}, err
	}
	return tool.Output{Text: "Untrusted MCP tool catalog:\n" + string(encoded), Metadata: map[string]any{"server": arguments.Server, "tool_count": len(listed)}}, nil
}

func (value *LazyCallTool) Spec() tool.Spec { return value.spec.Clone() }

func (value *LazyCallTool) SupportsParallelToolCalls() bool { return false }

func (value *LazyCallTool) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	if value == nil || value.manager == nil {
		return tool.Output{}, errors.New("lazy MCP call tool is nil")
	}
	var arguments lazyCallArguments
	if err := json.Unmarshal(invocation.Call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if err := validateSampleBinding(ctx, value.manager); err != nil {
		return tool.Output{}, err
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.Name == "" {
		return tool.Output{}, errors.New("MCP tool name is empty")
	}
	if len(arguments.Arguments) == 0 {
		arguments.Arguments = json.RawMessage(`{}`)
	}
	remote, err := value.manager.ListTools(ctx, arguments.Server)
	if err != nil {
		return tool.Output{}, err
	}
	found := false
	for _, candidate := range remote {
		if candidate.Name == arguments.Name {
			found = true
			break
		}
	}
	if !found {
		return tool.Output{}, fmt.Errorf("MCP tool %q is not exposed by server %q", arguments.Name, arguments.Server)
	}
	key := policy.MCPApprovalKey(arguments.Server, arguments.Name)
	if !value.rules.Allows(key) && value.approvals != nil {
		request, requestErr := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskHigh, "external MCP tool call requires approval")
		if requestErr != nil {
			return tool.Output{}, requestErr
		}
		label := arguments.Server + "/" + arguments.Name
		request.Presentation = policy.ExternalApprovalPresentation("Tool use", "Do you want to proceed?", "Yes, and don't ask again for "+label, label)
		if value.events != nil {
			if publishErr := value.events.Publish(ctx, event.ApprovalRequested{RequestID: request.ID, ToolName: request.ToolName, Risk: string(request.Risk), Reason: request.Reason}); publishErr != nil {
				return tool.Output{}, publishErr
			}
		}
		decision, decideErr := value.approvals.Decide(ctx, request)
		if decideErr != nil {
			return tool.Output{}, decideErr
		}
		if validateErr := decision.Validate(); validateErr != nil {
			return tool.Output{}, validateErr
		}
		if !decision.Allowed() {
			return tool.Output{ToolName: "mcp_call", Text: "MCP tool call denied"}, &mcpApprovalDeniedError{reason: decision.Reason}
		}
		if decision.Scope == policy.ApprovalSession {
			value.rules.Approve(key)
		}
	}
	result, err := value.manager.CallTool(ctx, arguments.Server, arguments.Name, arguments.Arguments)
	if err != nil {
		return tool.Output{}, err
	}
	text, partial := boundText(result.Text, value.maxBytes)
	text = "Untrusted external MCP result from " + arguments.Server + "/" + arguments.Name + ":\n" + text
	toolResult := tool.Output{Text: text, Partial: partial, Metadata: map[string]any{"server": arguments.Server, "tool": arguments.Name, "is_error": result.IsError}}
	if result.IsError {
		return toolResult, errors.New("MCP server returned tool error")
	}
	return toolResult, nil
}

type mcpApprovalDeniedError struct{ reason string }

func (err *mcpApprovalDeniedError) Error() string         { return "MCP tool call denied: " + err.reason }
func (err *mcpApprovalDeniedError) ToolErrorKind() string { return "approval_denied" }

func validateSampleBinding(ctx context.Context, manager *Manager) error {
	snapshot, ok := tool.RequestSnapshotFromContext(ctx)
	if !ok {
		return nil
	}
	return manager.ValidateBindingRevision(snapshot.MCPBindingRevision)
}

var _ tool.Handler = (*LazyListTool)(nil)
var _ tool.Handler = (*LazyCallTool)(nil)
