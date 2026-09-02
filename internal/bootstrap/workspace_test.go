package bootstrap

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type closeTrackingThreadStore struct {
	threadstore.ThreadStore
	closes atomic.Int32
}

func (store *closeTrackingThreadStore) Close() error {
	store.closes.Add(1)
	return store.ThreadStore.Close()
}

func TestOpenWorkspaceClosesStoreWhenCompositionFails(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateRuntime, err := DefaultStateRuntimeFactory(ctx, home, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer stateRuntime.Close()
	base, err := DefaultThreadStoreFactory(ctx, home, stateRuntime.Threads(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	tracking := &closeTrackingThreadStore{ThreadStore: base}
	_, err = OpenWorkspace(ctx, WorkspaceOptions{
		AmadeusRoot: home, CWD: t.TempDir(), Configuration: testConfiguration(),
		Dependencies: Dependencies{
			ThreadStore: func(context.Context, string, threadstore.MetadataDB, rollout.Clock) (threadstore.ThreadStore, error) {
				return tracking, nil
			},
			NextID: NextPersistentID,
		},
	})
	if err == nil || err.Error() != "bootstrap audit factory is nil" {
		t.Fatalf("unexpected composition error: %v", err)
	}
	if tracking.closes.Load() != 1 {
		t.Fatalf("failed composition closed store %d times", tracking.closes.Load())
	}
}

func TestCloseWorkspaceIsIdempotentAtApplicationBoundary(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateRuntime, err := DefaultStateRuntimeFactory(ctx, home, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer stateRuntime.Close()
	base, err := DefaultThreadStoreFactory(ctx, home, stateRuntime.Threads(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	tracking := &closeTrackingThreadStore{ThreadStore: base}
	result, err := OpenWorkspace(ctx, WorkspaceOptions{
		AmadeusRoot: home, CWD: t.TempDir(), Configuration: testConfiguration(),
		Dependencies: Dependencies{
			ThreadStore: func(context.Context, string, threadstore.MetadataDB, rollout.Clock) (threadstore.ThreadStore, error) {
				return tracking, nil
			},
			AuditFactory: func() (audit.Sink, io.Closer, error) {
				return nil, nil, errors.New("not reached without a started session")
			},
			NextID: NextPersistentID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseWorkspace(result.Workspace); err != nil {
		t.Fatal(err)
	}
	if err := CloseWorkspace(result.Workspace); err != nil {
		t.Fatal(err)
	}
	if tracking.closes.Load() != 1 {
		t.Fatalf("workspace close reached store %d times", tracking.closes.Load())
	}
}

func testConfiguration() config.Config {
	configured := config.Default()
	configured.Model = "model"
	configured.ModelProvider = "mock"
	configured.ModelContextWindow = 8192
	configured.ModelAutoCompactTokenLimit = 7372
	configured.ModelProviders = map[string]config.ModelProviderInfo{
		"mock": {
			WireAPI: config.WireAPIResponses, Dialect: config.DialectStandard,
			APIKey: "test", BaseURL: "https://example.invalid/v1",
			Timeout: time.Second, StreamIdleTimeout: time.Minute,
		},
	}
	return configured
}
