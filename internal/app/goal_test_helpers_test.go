package app

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/extension"
	goalextension "github.com/Godric-W/Amadeus/internal/extension/goal"
	"github.com/Godric-W/Amadeus/internal/state"
)

func appTestGoalRuntime(t *testing.T, runtime state.Runtime) (*extension.Registry, *goalextension.Service) {
	t.Helper()
	service, err := goalextension.NewService(runtime)
	if err != nil {
		t.Fatal(err)
	}
	builder := extension.NewBuilder(extension.NewEventRouter())
	if _, err := goalextension.Install(builder, runtime, service, time.Now); err != nil {
		t.Fatal(err)
	}
	return builder.Build(), service
}
