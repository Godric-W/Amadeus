package local

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type failOnceDeleteMetadataDB struct {
	threadstore.MetadataDB
	fail bool
}

func (db *failOnceDeleteMetadataDB) DeleteThread(ctx context.Context, id protocol.ThreadID) error {
	if db.fail {
		db.fail = false
		return errors.New("injected metadata delete failure")
	}
	return db.MetadataDB.DeleteThread(ctx, id)
}

func openMetadataStore(t *testing.T, ctx context.Context, home string) threadstore.MetadataDB {
	t.Helper()
	runtime, err := statesqlite.Open(ctx, home, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	return runtime.Threads()
}

func TestStoreDurableHistoryAndRebuild(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	store, err := NewStore(home, stateStore, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Materialize(ctx, threadstore.CreateInput{
		SessionID: testutil.SessionID(1), ID: testutil.ThreadID(1), CWD: "/workspace", Title: "Thread", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: now,
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
	usage := rollout.EventMsgItem{Msg: protocol.NewTokenCountEvent(llm.TokenUsage{TotalTokens: 42}, 128_000, 1)}
	result, err = store.AppendItems(ctx, testutil.ThreadID(1), "turn-1", response, usage)
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning != nil {
		t.Fatal(result.MetadataWarning)
	}
	metadata, err := store.GetThread(ctx, testutil.ThreadID(1))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "inspect files" || metadata.TokensUsed != 42 {
		t.Fatalf("metadata = %#v", metadata)
	}
	if err := stateStore.ReplaceThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	rename := rollout.EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{Name: "Recovered Index"}}
	moreUsageEvent := protocol.NewTokenCountEvent(llm.TokenUsage{TotalTokens: 8}, 128_000, 1)
	moreUsageEvent.Info.TotalTokenUsage.TotalTokens = 50
	moreUsage := rollout.EventMsgItem{Msg: moreUsageEvent}
	result, err = store.AppendItems(ctx, testutil.ThreadID(1), "", rename)
	if err != nil {
		t.Fatal(err)
	}
	result, err = store.AppendItems(ctx, testutil.ThreadID(1), "turn-2", moreUsage)
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning != nil {
		t.Fatal(result.MetadataWarning)
	}
	metadata, err = store.GetThread(ctx, testutil.ThreadID(1))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Title != "Recovered Index" || metadata.Preview != "inspect files" || metadata.TokensUsed != 50 {
		t.Fatalf("metadata after live backfill = %#v", metadata)
	}
	if err := store.CloseWriter(ctx, testutil.ThreadID(1)); err != nil {
		t.Fatal(err)
	}
	history, err := store.LoadHistory(ctx, testutil.ThreadID(1))
	if err != nil {
		t.Fatal(err)
	}
	if history.Kind != threadstore.InitialHistoryResumed || len(history.Lines) != 5 {
		t.Fatalf("history = %#v", history)
	}
	meta := history.Lines[0].Item.(rollout.SessionMetaItem)
	if meta.BaseInstructions != testutil.BaseInstructions("test-model") {
		t.Fatalf("session metadata base instructions = %#v", meta.BaseInstructions)
	}
	if err := stateStore.ReplaceThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RebuildIndex(ctx); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListThreads(ctx, threadstore.ListQuery{CWD: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != testutil.ThreadID(1) || listed[0].GitSHA != "abc123" || listed[0].GitBranch != "main" || listed[0].GitOriginURL != "git@example.com:amadeus.git" {
		t.Fatalf("rebuilt = %#v", listed)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsSecondActiveWriter(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: testutil.SessionID(1), ID: testutil.ThreadID(1), CWD: "/workspace", Title: "Thread", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenWriter(ctx, testutil.ThreadID(1)); err == nil {
		t.Fatal("second active writer was accepted")
	}
}

func TestDeleteThreadKeepsMetadataUntilRolloutDeleteCanBeRetried(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	failing := &failOnceDeleteMetadataDB{MetadataDB: stateStore, fail: true}
	store, err := NewStore(home, failing, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := testutil.ThreadID(91)
	if _, err := store.Materialize(ctx, threadstore.CreateInput{
		SessionID: protocol.SessionIDFromThreadID(id), ID: id, CWD: "/workspace", Title: "Delete me",
		BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(ctx, id); err != nil {
		t.Fatal(err)
	}
	metadata, err := stateStore.GetThread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteThread(ctx, id); err == nil || err.Error() != "delete thread metadata: injected metadata delete failure" {
		t.Fatalf("first delete error = %v", err)
	}
	if _, err := os.Stat(metadata.RolloutPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollout still exists after first delete: %v", err)
	}
	if _, err := stateStore.GetThread(ctx, id); err != nil {
		t.Fatalf("metadata was removed before the retry boundary: %v", err)
	}
	if err := store.DeleteThread(ctx, id); err != nil {
		t.Fatalf("retry delete: %v", err)
	}
	if _, err := stateStore.GetThread(ctx, id); !errors.Is(err, threadstore.ErrNotFound) {
		t.Fatalf("metadata after retry = %v", err)
	}
	if err := store.DeleteThread(ctx, id); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}

func TestRebuildIndexRestoresChildParentRelationWithoutSessionColumn(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	rootID, childID := testutil.ThreadID(10), testutil.ThreadID(11)
	sessionID := protocol.SessionIDFromThreadID(rootID)
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: sessionID, ID: rootID, Source: protocol.RootSessionSource(), CWD: "/workspace", Title: "Root", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: sessionID, ID: childID, Source: protocol.NewSubAgentSessionSource(rootID, 1, "atlas", "explorer"), CWD: "/workspace", Title: "Child", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItems(ctx, rootID, "", rollout.AgentSpawnEdgeItem{AgentID: childID, State: protocol.AgentSpawnEdgeOpen, UpdatedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(ctx, rootID); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(ctx, childID); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.ReplaceThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RebuildIndex(ctx); err != nil {
		t.Fatal(err)
	}
	children, err := store.ListOpenChildren(ctx, rootID)
	if err != nil || len(children) != 1 || children[0].ID != childID || children[0].Source.SubAgent.ParentThreadID != rootID {
		t.Fatalf("rebuilt children = %#v, err=%v", children, err)
	}
	history, err := store.LoadHistory(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	meta := history.Lines[0].Item.(rollout.SessionMetaItem)
	if meta.SessionID != sessionID || meta.ID != childID || meta.ParentThreadID == nil || *meta.ParentThreadID != rootID {
		t.Fatalf("rebuilt child session metadata = %#v", meta)
	}
}

func TestRebuildIndexKeepsExplicitlyClosedChildOutOfOpenChildren(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	rootID, childID := testutil.ThreadID(20), testutil.ThreadID(21)
	sessionID := protocol.SessionIDFromThreadID(rootID)
	for _, input := range []threadstore.CreateInput{
		{SessionID: sessionID, ID: rootID, Source: protocol.RootSessionSource(), CWD: "/workspace", Title: "Root", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: now},
		{SessionID: sessionID, ID: childID, Source: protocol.NewSubAgentSessionSource(rootID, 1, "atlas", "explorer"), CWD: "/workspace", Title: "Child", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: now},
	} {
		if _, err := store.Materialize(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	for index, state := range []protocol.AgentSpawnEdgeState{protocol.AgentSpawnEdgeOpen, protocol.AgentSpawnEdgeClosed} {
		if _, err := store.AppendItems(ctx, rootID, "", rollout.AgentSpawnEdgeItem{AgentID: childID, State: state, UpdatedAt: now.Add(time.Duration(index+1) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CloseWriter(ctx, rootID); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(ctx, childID); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.ReplaceThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RebuildIndex(ctx); err != nil {
		t.Fatal(err)
	}
	children, err := store.ListOpenChildren(ctx, rootID)
	if err != nil || len(children) != 0 {
		t.Fatalf("rebuilt open children = %#v, err=%v", children, err)
	}
	child, err := store.GetThread(ctx, childID)
	if err != nil || child.AgentEdgeState != protocol.AgentSpawnEdgeClosed {
		t.Fatalf("rebuilt closed child = %#v, err=%v", child, err)
	}
}

func TestBufferedAppendDoesNotAdvanceSQLiteBeforeDurableAppend(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: testutil.SessionID(2), ID: testutil.ThreadID(2), CWD: "/workspace", Title: "Thread", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "buffered preview"})
	if err != nil {
		t.Fatal(err)
	}
	usage := rollout.EventMsgItem{Msg: protocol.NewTokenCountEvent(llm.TokenUsage{TotalTokens: 17}, 128_000, 1)}
	if _, err := store.AppendItemsBuffered(ctx, testutil.ThreadID(2), "turn-1", response, usage); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.GetThread(ctx, testutil.ThreadID(2))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "" || metadata.TokensUsed != 0 {
		t.Fatalf("buffered metadata advanced before flush: %#v", metadata)
	}
	terminal := rollout.EventMsgItem{Msg: protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC()}}
	if _, err := store.AppendItems(ctx, testutil.ThreadID(2), "turn-1", terminal); err != nil {
		t.Fatal(err)
	}
	metadata, err = store.GetThread(ctx, testutil.ThreadID(2))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "buffered preview" || metadata.TokensUsed != 17 {
		t.Fatalf("durable metadata omitted buffered facts: %#v", metadata)
	}
}

func TestExplicitFlushAdvancesPendingMetadataAfterBufferedAppend(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := testutil.ThreadID(30)
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: testutil.SessionID(30), ID: id, CWD: "/workspace", Title: "Thread", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "flush me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItemsBuffered(ctx, id, "turn-1", response); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(ctx, id); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.GetThread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "flush me" {
		t.Fatalf("explicit flush did not sync metadata: %#v", metadata)
	}
}

func TestCloseWriterFlushesAndSyncsBufferedMetadata(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := testutil.ThreadID(31)
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: testutil.SessionID(31), ID: id, CWD: "/workspace", Title: "Thread", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "shutdown flush"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendItemsBuffered(ctx, id, "turn-1", response); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(ctx, id); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.GetThread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "shutdown flush" {
		t.Fatalf("normal writer close lost buffered metadata: %#v", metadata)
	}
}

type orderedRecorder struct {
	durableRecorder
	calls       *[]string
	appendError error
	flushError  error
}

func (recorder orderedRecorder) Append(ctx context.Context, items ...rollout.RolloutItem) ([]rollout.Line, error) {
	*recorder.calls = append(*recorder.calls, "append")
	if recorder.appendError != nil {
		return nil, recorder.appendError
	}
	return recorder.durableRecorder.Append(ctx, items...)
}

func (recorder orderedRecorder) Flush(ctx context.Context) error {
	*recorder.calls = append(*recorder.calls, "flush")
	if recorder.flushError != nil {
		return recorder.flushError
	}
	return recorder.durableRecorder.Flush(ctx)
}

type orderedStateDB struct {
	threadstore.MetadataDB
	calls       *[]string
	upsertError error
}

func (database orderedStateDB) UpsertThread(ctx context.Context, metadata threadstore.StoredThread) error {
	*database.calls = append(*database.calls, "upsert")
	if database.upsertError != nil {
		return database.upsertError
	}
	return database.MetadataDB.UpsertThread(ctx, metadata)
}

func TestDurableAppendOrdersAppendFlushAndMetadataSync(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	stateStore := openMetadataStore(t, ctx, home)
	store, err := NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Materialize(ctx, threadstore.CreateInput{SessionID: testutil.SessionID(3), ID: testutil.ThreadID(3), CWD: "/workspace", Title: "Thread", BaseInstructions: testutil.BaseInstructions("test-model"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "durable preview"})
	if err != nil {
		t.Fatal(err)
	}
	original := store.recorders[testutil.ThreadID(3)]

	calls := []string{}
	store.recorders[testutil.ThreadID(3)] = &writerState{recorder: orderedRecorder{durableRecorder: original.recorder, calls: &calls, appendError: errors.New("append failed")}, metadata: original.metadata}
	store.state = orderedStateDB{MetadataDB: stateStore, calls: &calls}
	if _, err := store.AppendItems(ctx, testutil.ThreadID(3), "turn-1", response); err == nil {
		t.Fatal("append failure was ignored")
	}
	if len(calls) != 1 || calls[0] != "append" {
		t.Fatalf("append failure ordering = %v", calls)
	}

	calls = nil
	store.recorders[testutil.ThreadID(3)] = &writerState{recorder: orderedRecorder{durableRecorder: original.recorder, calls: &calls, flushError: errors.New("flush failed")}, metadata: original.metadata}
	store.state = orderedStateDB{MetadataDB: stateStore, calls: &calls}
	if _, err := store.AppendItems(ctx, testutil.ThreadID(3), "turn-1", response); err == nil {
		t.Fatal("flush failure was ignored")
	}
	if len(calls) != 2 || calls[0] != "append" || calls[1] != "flush" {
		t.Fatalf("flush failure ordering = %v", calls)
	}
	metadata, err := stateStore.GetThread(ctx, testutil.ThreadID(3))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "" {
		t.Fatalf("metadata advanced beyond failed flush: %#v", metadata)
	}

	calls = nil
	store.recorders[testutil.ThreadID(3)] = &writerState{recorder: orderedRecorder{durableRecorder: original.recorder, calls: &calls}, metadata: original.metadata}
	store.state = orderedStateDB{MetadataDB: stateStore, calls: &calls, upsertError: errors.New("upsert failed")}
	terminal := rollout.EventMsgItem{Msg: protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC()}}
	result, err := store.AppendItems(ctx, testutil.ThreadID(3), "turn-1", terminal)
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataWarning == nil {
		t.Fatal("metadata upsert failure was not surfaced")
	}
	if len(calls) != 3 || calls[0] != "append" || calls[1] != "flush" || calls[2] != "upsert" {
		t.Fatalf("durable ordering = %v", calls)
	}
	metadata, err = stateStore.GetThread(ctx, testutil.ThreadID(3))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "" {
		t.Fatalf("failed metadata upsert advanced SQLite: %#v", metadata)
	}

	store.state = stateStore
	usage := rollout.EventMsgItem{Msg: protocol.NewTokenCountEvent(llm.TokenUsage{TotalTokens: 3}, 128_000, 1)}
	if _, err := store.AppendItems(ctx, testutil.ThreadID(3), "turn-2", usage); err != nil {
		t.Fatal(err)
	}
	metadata, err = stateStore.GetThread(ctx, testutil.ThreadID(3))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Preview != "durable preview" || metadata.TokensUsed != 3 {
		t.Fatalf("metadata reconciliation lost durable facts: %#v", metadata)
	}
}
