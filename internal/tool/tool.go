package tool

import "context"

type Handler interface {
	Spec() Spec
	SupportsParallelToolCalls() bool
	Handle(context.Context, Invocation) (Output, error)
}
