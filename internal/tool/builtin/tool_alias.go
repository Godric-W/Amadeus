package builtin

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type toolAlias struct {
	inner tool.Handler
	spec  tool.Spec
}

func newToolAlias(inner tool.Handler, name, description string, schema []byte) tool.Handler {
	spec := inner.Spec().Clone()
	spec.Name = name
	if description != "" {
		spec.Description = description
	}
	if len(schema) > 0 {
		spec.InputSchema = append([]byte(nil), schema...)
	}
	return &toolAlias{inner: inner, spec: spec}
}

func (alias *toolAlias) Spec() tool.Spec { return alias.spec.Clone() }
func (alias *toolAlias) SupportsParallelToolCalls() bool {
	return alias.inner.SupportsParallelToolCalls()
}
func (alias *toolAlias) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	invocation.Call.Name = alias.inner.Spec().Name
	output, err := alias.inner.Handle(ctx, invocation)
	output.ToolName = alias.spec.Name
	return output, err
}

var _ tool.Handler = (*toolAlias)(nil)

func NewGlobAlias(inner tool.Handler) tool.Handler {
	return newToolAlias(inner, "glob", "Find files below a directory using bounded glob matching.", globSpec().InputSchema)
}

func NewGrepAlias(inner tool.Handler) tool.Handler {
	return newToolAlias(inner, "grep", "Search project text with bounded matching and stable file/line output.", grepSpec().InputSchema)
}
