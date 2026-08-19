package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/thread/local"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

func TestInteractiveApplicationOwnsThreadLifecycleAndReplay(t *testing.T) {
	ctx := context.Background()
	workspace, configuration := newInteractiveTestWorkspace(t, ctx)
	application, err := NewInteractiveApplication(ctx, InteractiveOptions{
		Workspace: workspace, Configuration: configuration, Project: configuration.CWD,
		Provider: configuration.Provider, Model: configuration.Model, ContextWindow: 128000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	defer workspace.Close(context.Background())

	initial, err := application.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Generation != 1 || initial.ThreadID == "" {
		t.Fatalf("initial snapshot = %#v", initial)
	}
	firstID := initial.ThreadID

	if err := application.SetMode(ctx, turn.ModeKindPlan); err != nil {
		t.Fatal(err)
	}
	settings := waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		updated, ok := event.Event.Message.(protocol.ThreadSettingsUpdated)
		return ok && updated.Mode == string(turn.ModeKindPlan)
	})
	if settings.Generation != initial.Generation {
		t.Fatalf("settings generation = %d", settings.Generation)
	}

	if err := application.SubmitUser(ctx, "inspect repository"); err != nil {
		t.Fatal(err)
	}
	waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		_, ok := event.Event.Message.(protocol.TurnCompleted)
		return ok && event.Event.ThreadID == firstID
	})

	application.Clear(ctx)
	waitInteractiveEvent[ClearUIStarted](t, application.Events(), nil)
	cleared := waitInteractiveEvent[ThreadAttached](t, application.Events(), nil)
	if cleared.Snapshot.Generation != 2 || cleared.Snapshot.ThreadID == firstID || len(cleared.Snapshot.Items) != 0 {
		t.Fatalf("clear snapshot = %#v", cleared.Snapshot)
	}

	application.Resume(ctx, firstID)
	resumed := waitInteractiveEvent[ThreadAttached](t, application.Events(), nil)
	if resumed.Snapshot.Generation != 3 || resumed.Snapshot.ThreadID != firstID {
		t.Fatalf("resume snapshot = %#v", resumed.Snapshot)
	}
	wantKinds := []protocol.ItemKind{protocol.ItemUserMessage, protocol.ItemAssistantMessage}
	if len(resumed.Snapshot.Items) != len(wantKinds) {
		t.Fatalf("resume items = %#v", resumed.Snapshot.Items)
	}
	for index, kind := range wantKinds {
		if resumed.Snapshot.Items[index].Kind != kind {
			t.Fatalf("resume item %d = %q, want %q", index, resumed.Snapshot.Items[index].Kind, kind)
		}
	}
}

func TestInteractiveApplicationCompactPublishesTypedLifecycle(t *testing.T) {
	ctx := context.Background()
	workspace, configuration := newInteractiveTestWorkspace(t, ctx)
	application, err := NewInteractiveApplication(ctx, InteractiveOptions{Workspace: workspace, Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	defer workspace.Close(context.Background())
	if _, err := application.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := application.SubmitUser(ctx, "seed"); err != nil {
		t.Fatal(err)
	}
	waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		_, ok := event.Event.Message.(protocol.TurnCompleted)
		return ok
	})
	if err := application.SubmitCompact(ctx); err != nil {
		t.Fatal(err)
	}
	started := waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		value, ok := event.Event.Message.(protocol.TurnStarted)
		return ok && value.Kind == protocol.TaskKindCompact
	})
	compacted := waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		_, ok := event.Event.Message.(protocol.ContextCompacted)
		return ok
	})
	warning := waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		value, ok := event.Event.Message.(protocol.Warning)
		return ok && value.Message == "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."
	})
	completed := waitInteractiveEvent[SessionEventObserved](t, application.Events(), func(event SessionEventObserved) bool {
		_, ok := event.Event.Message.(protocol.TurnCompleted)
		return ok
	})
	if started.Generation != compacted.Generation || compacted.Generation != warning.Generation || warning.Generation != completed.Generation {
		t.Fatalf("compact generations = %d/%d/%d/%d", started.Generation, compacted.Generation, warning.Generation, completed.Generation)
	}
}

func newInteractiveTestWorkspace(t *testing.T, ctx context.Context) (*ThreadWorkspace, agentsession.Configuration) {
	t.Helper()
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
	var projectionMu sync.Mutex
	lastInput := ""
	constructor := func(kind agentsession.TaskKind) func(context.Context, *agentsession.Session, string, turn.TurnContext) (agentsession.SessionTask, turn.TurnContext, error) {
		return func(_ context.Context, _ *agentsession.Session, input string, value turn.TurnContext) (agentsession.SessionTask, turn.TurnContext, error) {
			return agentsession.FuncTask{TaskKind: kind, RunFunc: func(context.Context, *agentsession.Session, *turn.TurnContext, []agentsession.TurnInput) (agentsession.Result, error) {
				if kind == agentsession.TaskKindCompact {
					projectionMu.Lock()
					input := lastInput
					projectionMu.Unlock()
					encoded, encodeErr := json.Marshal([]llm.ResponseItem{llm.UserMessage(input), llm.AssistantMessage("done: " + input)})
					if encodeErr != nil {
						return agentsession.Result{}, encodeErr
					}
					digest := sha256.Sum256(encoded)
					item, itemErr := rollout.NewItem(rollout.KindCompaction, rollout.Compaction{
						Summary: "token budget", ReplacementHistory: []rollout.ReplacementMessage{{Role: "user", Content: "summary"}},
						CoveredThroughSequence: 100, SourceHash: hex.EncodeToString(digest[:]),
					})
					return agentsession.Result{Items: []rollout.Item{item}, Summary: "compacted"}, itemErr
				}
				projectionMu.Lock()
				lastInput = input
				projectionMu.Unlock()
				item, itemErr := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "done: " + input})
				return agentsession.Result{Items: []rollout.Item{item}, Summary: "completed"}, itemErr
			}}, value, nil
		}
	}
	manager, err := threadmanager.New(ctx, threadStore, threadmanager.SharedServices{
		DefaultSessionSetup: agentsession.SessionSetup{TaskConstructors: agentsession.TaskConstructors{
			Regular: constructor(agentsession.TaskKindRegular), Compact: constructor(agentsession.TaskKindCompact), Close: func() error { return nil },
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
	configuration := agentsession.Configuration{CWD: filepath.Clean(t.TempDir()), Provider: "mock", Model: "model", Mode: turn.ModeKindDefault}
	return workspace, configuration
}

func waitInteractiveEvent[T InteractiveEvent](t *testing.T, events <-chan InteractiveEvent, accept func(T) bool) T {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-events:
			value, ok := event.(T)
			if ok && (accept == nil || accept(value)) {
				return value
			}
		case <-timeout.C:
			var zero T
			t.Fatalf("timed out waiting for %T", zero)
		}
	}
}
