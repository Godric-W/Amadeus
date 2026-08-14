package local

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func TestStoreDurableHistoryAndRebuild(t *testing.T) {
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
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	store, err := NewStore(home, stateStore, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Materialize(ctx, thread.CreateInput{
		ID: "thread-1", CWD: "/workspace", Title: "Thread", CreatedAt: now,
		GitSHA: "abc123", GitBranch: "main", GitOriginURL: "git@example.com:amadeus.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning != nil {
		t.Fatal(result.MetadataWarning)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect files"})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{TotalTokens: 42})
	if err != nil {
		t.Fatal(err)
	}
	result, err = store.AppendItems(ctx, "thread-1", "turn-1", response, usage)
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning != nil {
		t.Fatal(result.MetadataWarning)
	}
	metadata, err := store.GetThread(ctx, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "inspect files" || metadata.TokensUsed != 42 {
		t.Fatalf("metadata = %#v", metadata)
	}
	if err := stateStore.ReplaceThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	rename, err := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{Title: "Recovered Index"})
	if err != nil {
		t.Fatal(err)
	}
	moreUsage, err := rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{TotalTokens: 8})
	if err != nil {
		t.Fatal(err)
	}
	result, err = store.AppendItems(ctx, "thread-1", "", rename)
	if err != nil {
		t.Fatal(err)
	}
	result, err = store.AppendItems(ctx, "thread-1", "turn-2", moreUsage)
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning != nil {
		t.Fatal(result.MetadataWarning)
	}
	metadata, err = store.GetThread(ctx, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Title != "Recovered Index" || metadata.Preview != "inspect files" || metadata.TokensUsed != 50 {
		t.Fatalf("metadata after live backfill = %#v", metadata)
	}
	if err := store.CloseWriter(ctx, "thread-1"); err != nil {
		t.Fatal(err)
	}
	history, err := store.LoadHistory(ctx, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if history.Kind != thread.InitialHistoryResumed || len(history.Lines) != 5 {
		t.Fatalf("history = %#v", history)
	}
	if err := stateStore.ReplaceThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RebuildIndex(ctx); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListThreads(ctx, state.ListQuery{CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "thread-1" || listed[0].GitSHA != "abc123" || listed[0].GitBranch != "main" || listed[0].GitOriginURL != "git@example.com:amadeus.git" {
		t.Fatalf("rebuilt = %#v", listed)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsSecondActiveWriter(t *testing.T) {
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
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Materialize(ctx, thread.CreateInput{ID: "thread-1", CWD: "/workspace", Title: "Thread", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenWriter(ctx, "thread-1"); err == nil {
		t.Fatal("second active writer was accepted")
	}
}

func TestBufferedAppendDoesNotAdvanceSQLiteBeforeDurableAppend(t *testing.T) {
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
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Materialize(ctx, thread.CreateInput{ID: "thread-buffered", CWD: "/workspace", Title: "Thread", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "buffered preview"})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{TotalTokens: 17})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItemsBuffered(ctx, "thread-buffered", "turn-1", response, usage); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.GetThread(ctx, "thread-buffered")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "" || metadata.TokensUsed != 0 {
		t.Fatalf("buffered metadata advanced before flush: %#v", metadata)
	}
	terminal, err := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItems(ctx, "thread-buffered", "turn-1", terminal); err != nil {
		t.Fatal(err)
	}
	metadata, err = store.GetThread(ctx, "thread-buffered")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "buffered preview" || metadata.TokensUsed != 17 {
		t.Fatalf("durable metadata omitted buffered facts: %#v", metadata)
	}
}

type orderedRecorder struct {
	durableRecorder
	calls       *[]string
	appendError error
	flushError  error
}

func (recorder orderedRecorder) Append(ctx context.Context, turnID rollout.TurnID, items ...rollout.Item) ([]rollout.Line, error) {
	*recorder.calls = append(*recorder.calls, "append")
	if recorder.appendError != nil {
		return nil, recorder.appendError
	}
	return recorder.durableRecorder.Append(ctx, turnID, items...)
}

func (recorder orderedRecorder) Flush(ctx context.Context) error {
	*recorder.calls = append(*recorder.calls, "flush")
	if recorder.flushError != nil {
		return recorder.flushError
	}
	return recorder.durableRecorder.Flush(ctx)
}

type orderedStateDB struct {
	state.DB
	calls       *[]string
	upsertError error
}

func (database orderedStateDB) UpsertThread(ctx context.Context, metadata state.StoredThread) error {
	*database.calls = append(*database.calls, "upsert")
	if database.upsertError != nil {
		return database.upsertError
	}
	return database.DB.UpsertThread(ctx, metadata)
}

func TestDurableAppendOrdersAppendFlushAndMetadataSync(t *testing.T) {
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
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Materialize(ctx, thread.CreateInput{ID: "thread-order", CWD: "/workspace", Title: "Thread", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "durable preview"})
	if err != nil {
		t.Fatal(err)
	}
	original := store.recorders["thread-order"]

	calls := []string{}
	store.recorders["thread-order"] = orderedRecorder{durableRecorder: original, calls: &calls, appendError: errors.New("append failed")}
	store.state = orderedStateDB{DB: stateStore, calls: &calls}
	if _, err := store.AppendItems(ctx, "thread-order", "turn-1", response); err == nil {
		t.Fatal("append failure was ignored")
	}
	if len(calls) != 1 || calls[0] != "append" {
		t.Fatalf("append failure ordering = %v", calls)
	}

	calls = nil
	store.recorders["thread-order"] = orderedRecorder{durableRecorder: original, calls: &calls, flushError: errors.New("flush failed")}
	store.state = orderedStateDB{DB: stateStore, calls: &calls}
	if _, err := store.AppendItems(ctx, "thread-order", "turn-1", response); err == nil {
		t.Fatal("flush failure was ignored")
	}
	if len(calls) != 2 || calls[0] != "append" || calls[1] != "flush" {
		t.Fatalf("flush failure ordering = %v", calls)
	}
	metadata, err := stateStore.GetThread(ctx, "thread-order")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "" {
		t.Fatalf("metadata advanced beyond failed flush: %#v", metadata)
	}

	calls = nil
	store.recorders["thread-order"] = orderedRecorder{durableRecorder: original, calls: &calls}
	store.state = orderedStateDB{DB: stateStore, calls: &calls, upsertError: errors.New("upsert failed")}
	terminal, err := rollout.NewItem(rollout.KindTurnCompleted, rollout.TurnCompleted{Status: rollout.TurnStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.AppendItems(ctx, "thread-order", "turn-1", terminal)
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning == nil {
		t.Fatal("metadata upsert failure was not surfaced")
	}
	if len(calls) != 3 || calls[0] != "append" || calls[1] != "flush" || calls[2] != "upsert" {
		t.Fatalf("durable ordering = %v", calls)
	}
	metadata, err = stateStore.GetThread(ctx, "thread-order")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "" {
		t.Fatalf("failed metadata upsert advanced SQLite: %#v", metadata)
	}

	store.state = stateStore
	usage, err := rollout.NewItem(rollout.KindTokenUsage, rollout.TokenUsage{TotalTokens: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItems(ctx, "thread-order", "turn-2", usage); err != nil {
		t.Fatal(err)
	}
	metadata, err = stateStore.GetThread(ctx, "thread-order")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "durable preview" || metadata.TokensUsed != 3 {
		t.Fatalf("metadata reconciliation lost durable facts: %#v", metadata)
	}
}
