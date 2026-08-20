package app

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/thread/local"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

type workspaceTaskSource struct{}

func (workspaceTaskSource) NewTask(_ context.Context, _ *agentsession.Session, kind agentsession.TaskKind, _ string, value turn.TurnContext) (agentsession.SessionTask, turn.TurnContext, error) {
	return agentsession.FuncTask{
		TaskKind: kind,
		RunFunc: func(context.Context, *agentsession.Session, *turn.TurnContext, []agentsession.TurnInput) (agentsession.Result, error) {
			return agentsession.Result{Summary: "completed"}, nil
		},
	}, value, nil
}

func (workspaceTaskSource) Close() error { return nil }

func TestThreadWorkspaceOwnsCurrentThreadLifecycle(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	database, err := statesqlite.Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	stateStore, err := statesqlite.NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	threadStore, err := local.NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sequence atomic.Uint64
	manager, err := threadmanager.New(ctx, threadStore, threadmanager.SharedServices{
		DefaultSessionSetup: agentsession.SessionSetup{TaskConstructors: agentsession.TaskConstructors{
			Regular: func(ctx context.Context, active *agentsession.Session, input string, value turn.TurnContext) (agentsession.SessionTask, turn.TurnContext, error) {
				return workspaceTaskSource{}.NewTask(ctx, active, agentsession.TaskKindRegular, input, value)
			},
			Compact: func(ctx context.Context, active *agentsession.Session, input string, value turn.TurnContext) (agentsession.SessionTask, turn.TurnContext, error) {
				return workspaceTaskSource{}.NewTask(ctx, active, agentsession.TaskKindCompact, input, value)
			},
			Close: workspaceTaskSource{}.Close,
		}},
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
	configuration := agentsession.Configuration{
		CWD: filepath.Clean(t.TempDir()), Provider: "mock", Model: "model",
		Mode: turn.ModeKindDefault,
	}
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
	resumed, err := workspace.Resume(ctx, id, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if resumed == current {
		t.Fatal("Resume reused the terminated thread runtime")
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
