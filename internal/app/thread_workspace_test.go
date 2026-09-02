package app

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/protocol"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	threadmanager "github.com/Godric-W/Amadeus/internal/threadmanager"
	"github.com/Godric-W/Amadeus/internal/threadstore/local"
)

func TestThreadWorkspaceOwnsCurrentThreadLifecycle(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateRuntime, err := statesqlite.Open(ctx, home, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	threadStore, err := local.NewStore(home, stateRuntime.Threads(), nil)
	if err != nil {
		t.Fatal(err)
	}
	extensions, goalService := appTestGoalRuntime(t, stateRuntime)
	var sequence atomic.Uint64
	manager, err := threadmanager.New(ctx, threadStore, threadmanager.SharedServices{
		State: stateRuntime, Extensions: extensions, GoalService: goalService,
		SessionAdapters: appSessionAdapters(t, &appTestClient{}),
		NextID: func(prefix string) string {
			return prefix + "-" + time.Unix(0, int64(sequence.Add(1))).UTC().Format("150405.000000000")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewThreadWorkspace(manager)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close(context.Background())
	configuration := appSessionConfiguration(t, filepath.Clean(t.TempDir()))
	current, err := workspace.EnsureCurrent(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	again, err := workspace.EnsureCurrent(ctx, configuration)
	if err != nil || again != current {
		t.Fatalf("EnsureCurrent created a second thread: current=%p again=%p err=%v", current, again, err)
	}
	if err := current.Submit(ctx, protocol.UserInputOp{Content: "materialize workspace"}); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceTerminal(t, current)
	if err := workspace.RenameCurrent(ctx, "Renamed Workspace"); err != nil {
		t.Fatal(err)
	}
	metadata, err := workspace.CurrentMetadata(ctx)
	if err != nil || metadata.Title != "Renamed Workspace" {
		t.Fatalf("current metadata = %#v err=%v", metadata, err)
	}
	id := current.ID()
	if err := workspace.NewDraft(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := workspace.Current(); ok {
		t.Fatal("NewDraft retained the previous current thread")
	}
	latest, err := workspace.PrepareStart(ctx, ThreadTarget{Kind: ThreadTargetLatest}, configuration)
	if err != nil || latest.Active == nil || latest.Metadata == nil || latest.Metadata.ID != id {
		t.Fatalf("PrepareStart latest = %#v, %v", latest, err)
	}
	resumed := latest.Active
	if resumed == current {
		t.Fatal("latest target reused the terminated thread runtime")
	}
	if err := workspace.NewDraft(ctx); err != nil {
		t.Fatal(err)
	}
	explicit, err := workspace.PrepareStart(ctx, ThreadTarget{Kind: ThreadTargetResume, ThreadID: id}, configuration)
	if err != nil || explicit.Active == nil || explicit.Metadata == nil || explicit.Metadata.ID != id {
		t.Fatalf("PrepareStart resume = %#v, %v", explicit, err)
	}
	deleted, err := workspace.DeleteCurrent(ctx)
	if err != nil || deleted != id {
		t.Fatalf("DeleteCurrent = %q, %v", deleted, err)
	}
	if _, ok := workspace.Current(); ok {
		t.Fatal("DeleteCurrent retained the deleted thread")
	}
}

func waitForWorkspaceTerminal(t *testing.T, value interface{ Io() agentsession.SessionIo }) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-value.Io().Events:
			switch event.Msg.(type) {
			case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
				return
			case protocol.StreamErrorEvent:
				t.Fatalf("stream error: %#v", event.Msg)
			}
		case <-timeout:
			t.Fatal("timed out waiting for terminal")
		}
	}
}
