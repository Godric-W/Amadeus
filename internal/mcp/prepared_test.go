package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func executePreparedTool(t testing.TB, ctx context.Context, candidate tool.Tool, arguments json.RawMessage) (tool.Result, error) {
	t.Helper()
	prepared, err := candidate.Prepare(ctx, tool.NewCall("test-call", candidate.Spec().Name, arguments))
	if err != nil {
		return tool.Result{}, err
	}
	return candidate.Execute(ctx, prepared)
}
