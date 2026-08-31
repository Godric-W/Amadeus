package app

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestInteractiveApplicationWaitsForTrackedAttachmentRelease(t *testing.T) {
	application := &InteractiveApplication{}
	application.releaseWG.Add(1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		application.releaseWG.Done()
	}()
	if err := application.waitForReleases(context.Background()); err != nil {
		t.Fatalf("wait for attachment release = %v", err)
	}
}

func TestInteractiveApplicationAttachmentReleaseWaitIsBounded(t *testing.T) {
	application := &InteractiveApplication{}
	application.releaseWG.Add(1)
	defer application.releaseWG.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := application.waitForReleases(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded release wait = %v", err)
	}
}
