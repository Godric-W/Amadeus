package rollout

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func BenchmarkReadLargeRollout(b *testing.B) {
	path := filepath.Join(b.TempDir(), "rollout.jsonl")
	threadID := testutil.ThreadID(1)
	recorder, err := Create(path, threadID, nil)
	if err != nil {
		b.Fatal(err)
	}
	items := make([]RolloutItem, 0, 5000)
	for index := 0; index < cap(items); index++ {
		items = append(items, ResponseItem{
			ThreadID: threadID, TurnID: protocol.TurnID("turn-benchmark"), Type: ResponseAssistantMessage,
			Role: "assistant", Content: "A representative response item used to measure streaming rollout recovery.",
		})
	}
	if _, err := recorder.Append(context.Background(), items...); err != nil {
		b.Fatal(err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		lines, err := Read(path, threadID)
		if err != nil {
			b.Fatal(err)
		}
		if len(lines) != len(items) {
			b.Fatalf("read %d lines, want %d", len(lines), len(items))
		}
	}
}

func BenchmarkOpenLargeRollout(b *testing.B) {
	path := filepath.Join(b.TempDir(), "rollout.jsonl")
	threadID := testutil.ThreadID(2)
	recorder, err := Create(path, threadID, nil)
	if err != nil {
		b.Fatal(err)
	}
	for index := 0; index < 5000; index++ {
		if _, err := recorder.Append(context.Background(), ResponseItem{
			ThreadID: threadID, TurnID: protocol.TurnID("turn-benchmark"), Type: ResponseUserMessage,
			Role: "user", Content: "benchmark input",
		}); err != nil {
			b.Fatal(err)
		}
	}
	if err := recorder.Close(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		opened, lines, err := Open(path, threadID, nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(lines) != 5000 {
			b.Fatalf("opened %d lines", len(lines))
		}
		if err := opened.Close(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
