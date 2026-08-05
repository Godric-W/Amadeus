package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPresentCallUsesStableSafeSummaries(t *testing.T) {
	tests := []struct {
		name   string
		spec   Spec
		input  string
		want   string
		detail string
	}{
		{name: "read", spec: Spec{Name: "read_file", SideEffect: SideEffectRead}, input: `{"path":"docs/design.md"}`, want: "Read docs/design.md"},
		{name: "search", spec: Spec{Name: "grep_code", SideEffect: SideEffectRead}, input: `{"query":"Plan.*Run","path":"internal"}`, want: "Search Plan.*Run in internal"},
		{name: "command", spec: Spec{Name: "execute_command", SideEffect: SideEffectExecute}, input: `{"command":"curl -H 'Authorization: Bearer secret' example.test"}`, want: "Ran curl -H 'Authorization: [REDACTED] [REDACTED] example.test"},
		{name: "web", spec: Spec{Name: "web_fetch", SideEffect: SideEffectNetwork}, input: `{"url":"https://example.test/private?q=secret"}`, want: "Fetched example.test"},
		{name: "mcp call", spec: Spec{Name: "mcp_call", SideEffect: SideEffectNetwork}, input: `{"server":"demo","name":"echo","arguments":{"api_key":"secret"}}`, want: "Called MCP tool", detail: "demo · echo"},
		{name: "mcp resource", spec: Spec{Name: "mcp_read_resource", SideEffect: SideEffectNetwork}, input: `{"server":"docs","uri":"file:///guide.md","headers":{"Authorization":"Bearer secret"}}`, want: "Read MCP resource docs", detail: "file:///guide.md"},
		{name: "unknown network", spec: Spec{Name: "remote", SideEffect: SideEffectNetwork}, input: `{}`, want: "Called network tool remote"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presentation := PresentCall(test.spec, NewCall("call", test.spec.Name, json.RawMessage(test.input)))
			if presentation.ActionSummary != test.want || presentation.Detail != test.detail {
				t.Fatalf("presentation = %#v, want summary=%q detail=%q", presentation, test.want, test.detail)
			}
			if strings.Contains(strings.ToLower(presentation.ActionSummary+" "+presentation.Detail), "secret") {
				t.Fatalf("presentation leaked secret: %#v", presentation)
			}
		})
	}
}
