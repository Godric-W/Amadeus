package tool

import "context"

type Tool interface {
	Spec() ToolSpec
	SupportsParallelToolCalls() bool
	Call(context.Context, Invocation) (Output, error)
}
