package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolRouterFreezesDefinitionAndParallelCapability(t *testing.T) {
	registry := NewRegistry()
	first := &executionServiceTestTool{
		name: "dynamic_tool", parallel: true,
		handle: func(context.Context, Invocation) (ToolResult, error) { return ToolResult{Text: "first"}, nil },
	}
	second := &executionServiceTestTool{
		name: "dynamic_tool", parallel: false,
		handle: func(context.Context, Invocation) (ToolResult, error) { return ToolResult{Text: "second"}, nil },
	}
	if err := registry.ReplaceDefinitionGroup("dynamic", []ToolDefinition{first}); err != nil {
		t.Fatal(err)
	}
	firstRouter := registry.SnapshotRouter(nil, RequestSnapshot{}, nil)
	if err := registry.ReplaceDefinitionGroup("dynamic", []ToolDefinition{second}); err != nil {
		t.Fatal(err)
	}
	secondRouter := registry.SnapshotRouter(nil, RequestSnapshot{}, nil)
	if firstRouter.Revision() == secondRouter.Revision() {
		t.Fatal("handler replacement did not change router revision")
	}
	if !firstRouter.SupportsParallelToolCalls("dynamic_tool") || secondRouter.SupportsParallelToolCalls("dynamic_tool") {
		t.Fatal("router did not freeze parallel capability")
	}
	service, err := NewToolExecutionService(registry, NewArgumentValidator(), ToolExecutionServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	call := NewCall("call-1", "dynamic_tool", json.RawMessage(`{"value":"ok"}`))
	firstResult, err := service.ExecuteBatchScoped(context.Background(), []ToolCall{call}, nil, ExecutionScope{Router: &firstRouter})
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := service.ExecuteBatchScoped(context.Background(), []ToolCall{call}, nil, ExecutionScope{Router: &secondRouter})
	if err != nil {
		t.Fatal(err)
	}
	if firstResult[0].Output.Text != "first" || secondResult[0].Output.Text != "second" {
		t.Fatalf("frozen dispatch changed identity: first=%#v second=%#v", firstResult, secondResult)
	}
}

func TestToolRouterRevisionIncludesCapabilitySnapshots(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDefinition(newFakeTool("read", true)); err != nil {
		t.Fatal(err)
	}
	base := registry.SnapshotRouter(nil, RequestSnapshot{MCPBindingRevision: "mcp-1", SkillRevision: "skill-1", AgentsMdRevision: "agents-1"}, nil)
	mcpChanged := registry.SnapshotRouter(nil, RequestSnapshot{MCPBindingRevision: "mcp-2", SkillRevision: "skill-1", AgentsMdRevision: "agents-1"}, nil)
	skillChanged := registry.SnapshotRouter(nil, RequestSnapshot{MCPBindingRevision: "mcp-1", SkillRevision: "skill-2", AgentsMdRevision: "agents-1"}, nil)
	agentsChanged := registry.SnapshotRouter(nil, RequestSnapshot{MCPBindingRevision: "mcp-1", SkillRevision: "skill-1", AgentsMdRevision: "agents-2"}, nil)
	if base.Revision() == mcpChanged.Revision() || base.Revision() == skillChanged.Revision() || base.Revision() == agentsChanged.Revision() {
		t.Fatal("router revision ignored MCP, Skill, or AGENTS.md snapshot")
	}
	if base.RequestSnapshot().ToolRouterRevision != base.Revision() {
		t.Fatalf("request snapshot is not bound to router: %#v", base.RequestSnapshot())
	}
}

func TestToolRouterSpecsAreImmutableCopies(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDefinition(newFakeTool("read", true)); err != nil {
		t.Fatal(err)
	}
	router := registry.SnapshotRouter(nil, RequestSnapshot{}, nil)
	specs := router.Specs()
	specs[0].InputSchema[0] = '['
	if router.Specs()[0].InputSchema[0] == '[' {
		t.Fatal("router spec mutation changed frozen snapshot")
	}
}
