package tool

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Spec() Spec
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
}

type Executor interface {
	Execute(ctx context.Context, call Call) (Result, error)
}

type Authorizer interface {
	Authorize(ctx context.Context, spec Spec, call Call) error
}
