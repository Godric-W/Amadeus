package mcp

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/tool"
)

const defaultResultBytes = 128 << 10

func boundText(value string, maximum int) (string, bool) {
	if maximum <= 0 || len(value) <= maximum {
		return value, false
	}
	return value[:maximum], true
}

func errorResult(toolName string, metadata map[string]any, err error) tool.ToolResult {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	kind := "mcp_error"
	var provider interface{ ToolErrorKind() string }
	if errors.As(err, &provider) && provider.ToolErrorKind() != "" {
		kind = provider.ToolErrorKind()
	}
	metadata["error_kind"] = kind
	metadata["error"] = err.Error()
	return tool.ToolResult{
		ToolName: toolName, Text: err.Error(), Metadata: metadata,
		Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: toolName, Summary: kind},
	}
}
