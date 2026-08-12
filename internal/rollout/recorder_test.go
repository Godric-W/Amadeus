package rollout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecorderConcurrentAppendAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	recorder, err := Create(path, "thread-1", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewRawItem(KindResponseItem, json.RawMessage(`{"role":"assistant","content":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 32)
	for index := range 32 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, appendErr := recorder.Append(context.Background(), TurnID(fmt.Sprintf("turn-%d", index)), item)
			errors <- appendErr
		}(index)
	}
	wait.Wait()
	close(errors)
	for appendErr := range errors {
		if appendErr != nil {
			t.Fatal(appendErr)
		}
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, lines, err := Open(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(context.Background())
	if len(lines) != 32 {
		t.Fatalf("line count = %d, want 32", len(lines))
	}
	for index, line := range lines {
		if line.Sequence != uint64(index+1) {
			t.Fatalf("sequence[%d] = %d", index, line.Sequence)
		}
	}
}

func TestRecorderIgnoresAndTruncatesIncompleteTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	recorder, err := Create(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewItem(KindContextUpdate, ContextUpdate{Title: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Append(context.Background(), "", item); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"version":1,"sequence":2`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, lines, err := Open(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(lines))
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if content[len(content)-1] != '\n' {
		t.Fatal("damaged tail was not truncated")
	}
}

func TestRecorderRepairsCompleteFinalLineWithoutNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	recorder, err := Create(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewItem(KindContextUpdate, ContextUpdate{Title: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Append(context.Background(), "", item); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.TrimSuffix(content, []byte{'\n'}), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, lines, err := Open(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	second, err := NewItem(KindContextUpdate, ContextUpdate{Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Append(context.Background(), "", second); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, err = Read(path, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines after append = %d, want 2", len(lines))
	}
}

func TestRecorderCloseReleasesFileAfterCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	recorder, err := Create(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := recorder.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v, want context canceled", err)
	}
	if _, err := recorder.Append(context.Background(), "", Item{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("append after cancelled close = %v", err)
	}
}

func TestRecorderPreservesUnknownPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	recorder, err := Create(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"future":{"enabled":true}}`)
	item, err := NewRawItem("future_item", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Append(context.Background(), "turn-1", item); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, err := Read(path, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Item.Kind != "future_item" || string(lines[0].Item.Payload) != string(payload) {
		t.Fatalf("unknown item was not preserved: %#v", lines)
	}
}

func TestRecorderHonorsCancelledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	recorder, err := Create(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	item, err := NewItem(KindContextUpdate, ContextUpdate{Title: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Append(ctx, "", item); err == nil {
		t.Fatal("cancelled append succeeded")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("cancelled append wrote %d bytes", len(content))
	}
}

func TestRecorderRejectsCorruptionBeforeTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(path, "thread-1", nil); err == nil {
		t.Fatal("corrupt non-tail line was accepted")
	}
}

func TestRecorderRejectsUnsafeThreadID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if _, err := Create(path, "../outside", nil); err == nil {
		t.Fatal("unsafe thread ID was accepted")
	}
}
