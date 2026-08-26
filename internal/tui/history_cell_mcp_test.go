package tui

import (
	"testing"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/mcp"
)

func TestMCPInventoryCodexSnapshots(t *testing.T) {
	for name, test := range map[string]struct {
		cell HistoryCell
		want string
	}{
		"empty": {
			cell: NewEmptyMCPInventoryCell(),
			want: "🔌  MCP Tools\n\n  • No MCP servers configured.",
		},
		"summary": {
			cell: NewMCPInventoryCell(application.MCPDetailSummary, application.MCPInventory{Servers: []application.MCPServerStatus{{
				Name: "plugin_docs", Enabled: true, AuthStatus: application.MCPAuthUnknown,
				Tools: []mcp.MCPToolMetadata{{Name: "lookup"}},
			}}}),
			want: "🔌  MCP Tools\n\n  • plugin_docs\n    • Auth: Unknown\n    • Tools: lookup",
		},
		"verbose": {
			cell: NewMCPInventoryCell(application.MCPDetailVerbose, application.MCPInventory{Servers: []application.MCPServerStatus{{
				Name: "plugin_docs", Enabled: true, AuthStatus: application.MCPAuthUnsupported,
				Tools:     []mcp.MCPToolMetadata{{Name: "lookup"}},
				Resources: []mcp.MCPResourceMetadata{{Name: "Docs", URI: "file:///docs"}},
			}}}),
			want: "🔌  MCP Tools\n\n  • plugin_docs\n    • Auth: Unsupported\n    • Tools: lookup\n    • Resources: Docs (file:///docs)\n    • Resource templates: (none)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := renderHistoryCellForTest(test.cell, noColorRenderContext()); got != test.want {
				t.Fatalf("snapshot mismatch\n got: %q\nwant: %q", got, test.want)
			}
		})
	}
}

func TestMCPCommandUsesCommandSemantic(t *testing.T) {
	lines := NewMCPCommandHistoryCell().DisplayLines(noColorRenderContext())
	if len(lines) != 1 || len(lines[0]) != 1 || lines[0][0].Text != "/mcp" || lines[0][0].Style != styleCommand {
		t.Fatalf("command projection = %#v", lines)
	}
}
