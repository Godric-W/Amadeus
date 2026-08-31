package local

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func BenchmarkMetadataProjectionFullScan(b *testing.B) {
	lines := benchmarkMetadataLines(b, 5000)
	path := "/workspace/rollout-" + testutil.ThreadID(40).String() + ".jsonl"
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := projectMetadata(path, lines); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMetadataSyncIncremental(b *testing.B) {
	lines := benchmarkMetadataLines(b, 5000)
	metadata, err := projectMetadata("/workspace/rollout-"+testutil.ThreadID(40).String()+".jsonl", lines[:1])
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		syncState := newMetadataSync(metadata)
		if err := syncState.observe(lines[1:]); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkMetadataLines(b *testing.B, count int) []rollout.Line {
	b.Helper()
	id := testutil.ThreadID(40)
	now := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	meta := rollout.SessionMetaItem{SessionID: testutil.SessionID(40), ID: id, Source: protocol.RootSessionSource(), CWD: "/workspace", Title: "Benchmark", BaseInstructions: testutil.BaseInstructions("model"), CreatedAt: now}
	lines := []rollout.Line{{Version: rollout.CurrentVersion, Sequence: 1, Timestamp: now, Item: meta}}
	for index := 0; index < count; index++ {
		item, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "benchmark metadata preview"})
		if err != nil {
			b.Fatal(err)
		}
		lines = append(lines, rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(index + 2), Timestamp: now.Add(time.Duration(index+1) * time.Millisecond), Item: item})
	}
	return lines
}
