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
