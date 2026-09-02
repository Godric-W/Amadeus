package bootstrap

import (
	"context"
	"time"

	"github.com/Godric-W/Amadeus/internal/state"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
)

type StateRuntimeFactory func(context.Context, string, func() time.Time) (state.Runtime, error)

func DefaultStateRuntimeFactory(ctx context.Context, amadeusRoot string, clock func() time.Time) (state.Runtime, error) {
	return statesqlite.Open(ctx, amadeusRoot, clock)
}
