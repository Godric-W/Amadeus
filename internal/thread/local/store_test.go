package local

import (
	"context"
	"encoding/json"
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
	response, err := rollout.NewRawItem(rollout.KindResponseItem, json.RawMessage(`{"role":"user","content":"inspect files"}`))
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
