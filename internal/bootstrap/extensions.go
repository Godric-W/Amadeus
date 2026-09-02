package bootstrap

import (
	"time"

	"github.com/Godric-W/Amadeus/internal/extension"
	goalextension "github.com/Godric-W/Amadeus/internal/extension/goal"
	"github.com/Godric-W/Amadeus/internal/state"
)

func buildExtensions(runtime state.Runtime, clock func() time.Time) (*extension.Registry, *goalextension.Service, error) {
	service, err := goalextension.NewService(runtime)
	if err != nil {
		return nil, nil, err
	}
	builder := extension.NewBuilder(extension.NewEventRouter())
	if _, err := goalextension.Install(builder, runtime, service, clock); err != nil {
		return nil, nil, err
	}
	return builder.Build(), service, nil
}
