package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func TestStoreThreadLifecycle(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	database, err := Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	thread := threadstore.StoredThread{
		ID: testutil.ThreadID(1), Source: protocol.RootSessionSource(), RolloutPath: filepath.Join(home, "sessions", "rollout.jsonl"), CWD: "/workspace",
		Title: "First", Preview: "hello", ModelProvider: "openai", Model: "gpt", TokensUsed: 12,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.UpsertThread(ctx, thread); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "First" || loaded.TokensUsed != 12 {
		t.Fatalf("loaded = %#v", loaded)
	}
	if err := store.RenameThread(ctx, thread.ID, "Renamed", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListThreads(ctx, threadstore.ListQuery{CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Title != "Renamed" {
		t.Fatalf("listed = %#v", listed)
	}
	if err := store.ArchiveThread(ctx, thread.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	listed, err = store.ListThreads(ctx, threadstore.ListQuery{CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("active list = %#v", listed)
	}
	listed, err = store.ListThreads(ctx, threadstore.ListQuery{CWD: "/workspace", IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Archived {
		t.Fatalf("archived list = %#v", listed)
	}
	if _, err := store.GetThread(ctx, testutil.ThreadID(99)); !errors.Is(err, threadstore.ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
}

func TestReplaceThreadsRebuildsIndex(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	threads := []threadstore.StoredThread{{
		ID: testutil.ThreadID(2), Source: protocol.RootSessionSource(), RolloutPath: "/tmp/rollout-2.jsonl", CWD: "/workspace", Title: "Second",
		CreatedAt: now, UpdatedAt: now,
	}}
	if err := store.ReplaceThreads(ctx, threads); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListThreads(ctx, threadstore.ListQuery{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != testutil.ThreadID(2) {
		t.Fatalf("listed = %#v", listed)
	}
}

func TestListThreadsExcludesSubagentsByDefault(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	database, err := Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	rootID, childID := testutil.ThreadID(1), testutil.ThreadID(2)
	root := threadstore.StoredThread{ID: rootID, Source: protocol.RootSessionSource(), RolloutPath: filepath.Join(home, "root.jsonl"), CWD: "/workspace", Title: "Root", CreatedAt: now, UpdatedAt: now}
	child := threadstore.StoredThread{ID: childID, Source: protocol.NewSubAgentSessionSource(rootID, 1, "atlas", "explorer"), AgentEdgeState: protocol.AgentSpawnEdgeOpen, RolloutPath: filepath.Join(home, "child.jsonl"), CWD: "/workspace", Title: "Child", CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertThread(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertThread(ctx, child); err != nil {
		t.Fatal(err)
	}
	topLevel, err := store.ListThreads(ctx, threadstore.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(topLevel) != 1 || topLevel[0].ID != rootID {
		t.Fatalf("top-level threads = %#v", topLevel)
	}
	all, err := store.ListThreads(ctx, threadstore.ListQuery{IncludeSubAgents: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all threads = %#v", all)
	}
	children, err := store.ListOpenChildren(ctx, rootID)
	if err != nil || len(children) != 1 || children[0].ID != childID {
		t.Fatalf("children = %#v, err=%v", children, err)
	}
	if err := store.UpdateAgentEdgeState(ctx, childID, protocol.AgentSpawnEdgeClosed, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	children, err = store.ListOpenChildren(ctx, rootID)
	if err != nil || len(children) != 0 {
		t.Fatalf("closed children = %#v, err=%v", children, err)
	}
	closed, err := store.GetThread(ctx, childID)
	if err != nil || closed.AgentEdgeState != protocol.AgentSpawnEdgeClosed {
		t.Fatalf("closed child metadata = %#v, err=%v", closed, err)
	}
}
