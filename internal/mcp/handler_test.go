package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func executePreparedTool(t testing.TB, ctx context.Context, candidate tool.Handler, arguments json.RawMessage) (tool.Output, error) {
	t.Helper()
	return candidate.Handle(ctx, tool.Invocation{Call: tool.NewCall("test-call", candidate.Spec().Name, arguments), Source: tool.ToolCallSourceModel})
}
