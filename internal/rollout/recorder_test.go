package rollout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func TestRecorderConcurrentAppendAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	now := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	recorder, err := Create(path, "thread-1", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 32)
	for index := range 32 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			item := ResponseItem{ThreadID: "thread-1", TurnID: protocol.TurnID(fmt.Sprintf("turn-%d", index)), Type: ResponseAssistantMessage, Role: "assistant", Content: "ok"}
			_, appendErr := recorder.Append(context.Background(), item)
			errorsChannel <- appendErr
		}(index)
	}
	wait.Wait()
	close(errorsChannel)
	for appendErr := range errorsChannel {
		if appendErr != nil {
			t.Fatal(appendErr)
		}
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
	recorder := createRecorderWithEvent(t, path, protocol.ThreadNameUpdatedEvent{ThreadID: "thread-1", Name: "updated"})
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"version":2,"sequence":2`); err != nil {
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
	recorder := createRecorderWithEvent(t, path, protocol.ThreadNameUpdatedEvent{ThreadID: "thread-1", Name: "first"})
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
	if _, err := reopened.Append(context.Background(), EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{ThreadID: "thread-1", Name: "second"}}); err != nil {
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
	item := EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{ThreadID: "thread-1", Name: "updated"}}
	if _, err := recorder.Append(context.Background(), item); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("append after cancelled close = %v", err)
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
	item := EventMsgItem{Msg: protocol.ThreadNameUpdatedEvent{ThreadID: "thread-1", Name: "updated"}}
	if _, err := recorder.Append(ctx, item); err == nil {
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

func TestRecorderRejectsUnsupportedAndCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(path, "thread-1", nil); err == nil {
		t.Fatal("corrupt non-tail line was accepted")
	}

	unsupported := filepath.Join(t.TempDir(), "unsupported.jsonl")
	content := `{"version":2,"sequence":1,"timestamp":"2026-08-20T01:02:03Z","type":"future_item","payload":{}}` + "\n"
	if err := os.WriteFile(unsupported, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(unsupported, "thread-1"); err == nil || !strings.Contains(err.Error(), "unsupported rollout item type") {
		t.Fatalf("unsupported item error = %v", err)
	}
}

func TestRecorderRejectsUnsafeThreadID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if _, err := Create(path, "../outside", nil); err == nil {
		t.Fatal("unsafe thread ID was accepted")
	}
}

func createRecorderWithEvent(t *testing.T, path string, event protocol.EventMsg) *Recorder {
	t.Helper()
	recorder, err := Create(path, "thread-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Append(context.Background(), EventMsgItem{Msg: event}); err != nil {
		t.Fatal(err)
	}
	return recorder
}
