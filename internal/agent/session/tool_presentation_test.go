package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestPresentCallUsesStableSafeSummaries(t *testing.T) {
	tests := []struct {
		name   string
		spec   tool.ToolSpec
		input  string
		want   string
		detail string
	}{
		{name: "read", spec: tool.ToolSpec{Name: "read", SideEffect: tool.SideEffectRead}, input: `{"path":"docs/design.md"}`, want: "Read docs/design.md"},
		{name: "search", spec: tool.ToolSpec{Name: "grep", SideEffect: tool.SideEffectRead}, input: `{"query":"Plan.*Run","path":"internal"}`, want: "Search Plan.*Run in internal"},
		{name: "write", spec: tool.ToolSpec{Name: "write", SideEffect: tool.SideEffectWrite}, input: `{"path":"internal/new.go","content":"package internal"}`, want: "Create internal/new.go"},
		{name: "edit", spec: tool.ToolSpec{Name: "edit", SideEffect: tool.SideEffectWrite}, input: `{"path":"internal/new.go","old_string":"old","new_string":"new"}`, want: "Update internal/new.go"},
		{name: "command", spec: tool.ToolSpec{Name: "execute_command", SideEffect: tool.SideEffectExecute}, input: `{"command":"curl -H 'Authorization: Bearer secret' example.test"}`, want: "Ran curl -H 'Authorization: [REDACTED] [REDACTED] example.test"},
		{name: "web", spec: tool.ToolSpec{Name: "web_fetch", SideEffect: tool.SideEffectNetwork}, input: `{"url":"https://example.test/private?q=secret"}`, want: "Fetched example.test"},
		{name: "mcp call", spec: tool.ToolSpec{Name: "mcp_call", SideEffect: tool.SideEffectNetwork}, input: `{"server":"demo","name":"echo","arguments":{"api_key":"secret"}}`, want: "Called MCP tool", detail: "demo · echo"},
		{name: "mcp resource", spec: tool.ToolSpec{Name: "mcp_read_resource", SideEffect: tool.SideEffectNetwork}, input: `{"server":"docs","uri":"file:///guide.md","headers":{"Authorization":"Bearer secret"}}`, want: "Read MCP resource docs", detail: "file:///guide.md"},
		{name: "unknown network", spec: tool.ToolSpec{Name: "remote", SideEffect: tool.SideEffectNetwork}, input: `{}`, want: "Called network tool remote"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presentation := presentToolCall(test.spec, tool.NewCall("call", test.spec.Name, json.RawMessage(test.input)))
			if presentation.ActionSummary != test.want || presentation.Detail != test.detail {
				t.Fatalf("presentation = %#v, want summary=%q detail=%q", presentation, test.want, test.detail)
			}
			if strings.Contains(strings.ToLower(presentation.ActionSummary+" "+presentation.Detail), "secret") {
				t.Fatalf("presentation leaked secret: %#v", presentation)
			}
		})
	}
}
