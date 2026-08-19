package tui

import (
	"sort"
	"strings"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/mcp"
)

type MCPCommandHistoryCell struct{}

func NewMCPCommandHistoryCell() HistoryCell { return MCPCommandHistoryCell{} }

func (MCPCommandHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	return []styledLine{{{Text: "/mcp", Style: styleCommand}}}
}

func (MCPCommandHistoryCell) RawLines() []string         { return []string{"/mcp"} }
func (MCPCommandHistoryCell) IsStreamContinuation() bool { return false }

type MCPInventoryCell struct {
	Detail    application.MCPDetail
	Inventory application.MCPInventory
}

func NewMCPInventoryCell(detail application.MCPDetail, inventory application.MCPInventory) HistoryCell {
	return MCPInventoryCell{Detail: detail, Inventory: inventory}
}

func NewEmptyMCPInventoryCell() HistoryCell {
	return MCPInventoryCell{Detail: application.MCPDetailSummary}
}

func (cell MCPInventoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	lines := []styledLine{
		{{Text: "🔌  "}, {Text: "MCP Tools", Style: styleBold}},
		{},
	}
	servers := sortedMCPServers(cell.Inventory.Servers)
	if len(servers) == 0 {
		return append(lines, styledLine{{Text: "  • No MCP servers configured.", Style: styleDim}})
	}
	if !mcpInventoryHasTools(servers) {
		lines = append(lines, styledLine{{Text: "  • No MCP tools available.", Style: styleDim}}, styledLine{})
	}
	for index, server := range servers {
		header := styledLine{{Text: "  • "}, {Text: server.Name}}
		if !server.Enabled {
			header = append(header, styledSpan{Text: " (disabled)", Style: styleFailure})
			lines = append(lines, header)
		} else {
			lines = append(lines, header)
			lines = append(lines, styledLine{{Text: "    • Auth: "}, {Text: mcpAuthLabel(server.AuthStatus)}})
			lines = append(lines, styledLine{{Text: "    • Tools: "}, {Text: mcpToolNames(server.Tools)}})
			if strings.TrimSpace(server.Error) != "" {
				lines = append(lines, styledLine{{Text: "    • Error: "}, {Text: sanitizeFullscreenContent(server.Error), Style: styleFailure}})
			}
			if cell.Detail == application.MCPDetailVerbose {
				lines = append(lines, styledLine{{Text: "    • Resources: "}, {Text: mcpResourceNames(server.Resources)}})
				lines = append(lines, styledLine{{Text: "    • Resource templates: "}, {Text: "(none)"}})
			}
		}
		if index < len(servers)-1 {
			lines = append(lines, styledLine{})
		}
	}
	return lines
}

func (cell MCPInventoryCell) RawLines() []string {
	lines := []string{"🔌  MCP Tools", ""}
	servers := sortedMCPServers(cell.Inventory.Servers)
	if len(servers) == 0 {
		return append(lines, "  • No MCP servers configured.")
	}
	if !mcpInventoryHasTools(servers) {
		lines = append(lines, "  • No MCP tools available.", "")
	}
	for index, server := range servers {
		header := "  • " + server.Name
		if !server.Enabled {
			lines = append(lines, header+" (disabled)")
		} else {
			lines = append(lines, header)
			lines = append(lines, "    • Auth: "+mcpAuthLabel(server.AuthStatus))
			lines = append(lines, "    • Tools: "+mcpToolNames(server.Tools))
			if strings.TrimSpace(server.Error) != "" {
				lines = append(lines, "    • Error: "+sanitizeFullscreenContent(server.Error))
			}
			if cell.Detail == application.MCPDetailVerbose {
				lines = append(lines, "    • Resources: "+mcpResourceNames(server.Resources))
				lines = append(lines, "    • Resource templates: (none)")
			}
		}
		if index < len(servers)-1 {
			lines = append(lines, "")
		}
	}
	return lines
}

func (MCPInventoryCell) IsStreamContinuation() bool { return false }

func sortedMCPServers(values []application.MCPServerStatus) []application.MCPServerStatus {
	servers := append([]application.MCPServerStatus(nil), values...)
	sort.Slice(servers, func(left, right int) bool { return servers[left].Name < servers[right].Name })
	return servers
}

func mcpInventoryHasTools(servers []application.MCPServerStatus) bool {
	for _, server := range servers {
		if len(server.Tools) > 0 {
			return true
		}
	}
	return false
}

func mcpAuthLabel(status application.MCPAuthStatus) string {
	if strings.TrimSpace(string(status)) == "" {
		return string(application.MCPAuthUnknown)
	}
	return string(status)
}

func mcpToolNames(tools []mcp.MCPToolMetadata) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func mcpResourceNames(resources []mcp.MCPResourceMetadata) string {
	values := append([]mcp.MCPResourceMetadata(nil), resources...)
	sort.Slice(values, func(left, right int) bool { return values[left].URI < values[right].URI })
	if len(values) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(values))
	for _, resource := range values {
		name := strings.TrimSpace(resource.Name)
		if name == "" {
			name = resource.URI
		}
		names = append(names, name+" ("+resource.URI+")")
	}
	return strings.Join(names, ", ")
}
