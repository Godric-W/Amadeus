package app

import (
	"context"
	"errors"
	"testing"
)

func TestInteractiveApplicationShutdownClosesWorkspaceAndDetaches(t *testing.T) {
	ctx := context.Background()
	workspace, configuration := newInteractiveTestWorkspace(t, ctx)
	application, err := NewInteractiveApplication(ctx, InteractiveOptions{Workspace: workspace, Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	if _, err := application.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := application.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := application.current(); !errors.Is(err, ErrNoActiveThread) {
		t.Fatalf("current after shutdown = %v", err)
	}
	if _, ok := workspace.Current(); ok {
		t.Fatal("workspace retained current thread after shutdown")
	}
	if err := application.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown = %v", err)
	}
}
