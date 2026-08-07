package tool

import (
	"context"
)

type Tool interface {
	Spec() Spec
	Prepare(ctx context.Context, call Call) (PreparedCall, error)
	Execute(ctx context.Context, prepared PreparedCall) (Result, error)
}

type Executor interface {
	Execute(ctx context.Context, call Call) (Result, error)
}

type Authorizer interface {
	Authorize(ctx context.Context, spec Spec, prepared PreparedCall) error
}
