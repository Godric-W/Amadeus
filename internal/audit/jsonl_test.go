package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJSONLSinkWritesQueryableRedactedRecords(t *testing.T) {
	var output bytes.Buffer
	sink, err := NewJSONLSink(&output)
	if err != nil {
		t.Fatalf("create JSONL sink: %v", err)
	}
	record := validAuditRecord()
	record.Reason = `Authorization: Bearer auth-secret api_key="key-secret" ` + strings.Repeat("large-body-", 100)
	if err := sink.Write(context.Background(), record); err != nil {
		t.Fatalf("write JSONL record: %v", err)
	}

	logged := output.String()
	for _, secret := range []string{"auth-secret", "key-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("audit JSONL leaked %q: %s", secret, logged)
		}
	}
	if strings.Contains(logged, strings.Repeat("large-body-", 60)) || !strings.Contains(logged, "[REDACTED]") {
		t.Fatalf("audit JSONL did not redact or bound large text: %s", logged)
	}
	var decoded Record
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &decoded); err != nil {
		t.Fatalf("decode queryable JSONL record: %v", err)
	}
	if decoded.ToolName != record.ToolName || decoded.Outcome != OutcomeAllow || decoded.Source != "user" || decoded.DurationMS != 42 {
		t.Fatalf("unexpected decoded audit record: %#v", decoded)
	}
	if strings.Contains(logged, "arguments\"") || strings.Contains(logged, "content\"") || strings.Contains(logged, "headers\"") {
		t.Fatalf("audit schema unexpectedly contains raw body fields: %s", logged)
	}
}

func TestJSONLSinkSerializesConcurrentWritesAsWholeLines(t *testing.T) {
	var output bytes.Buffer
	sink, err := NewJSONLSink(&output)
	if err != nil {
		t.Fatalf("create JSONL sink: %v", err)
	}
	const count = 64
	var waitGroup sync.WaitGroup
	for index := 0; index < count; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			record := validAuditRecord()
			record.RequestID = "request-" + string(rune('A'+index%26))
			if err := sink.Write(context.Background(), record); err != nil {
				t.Errorf("write concurrent audit record: %v", err)
			}
		}()
	}
	waitGroup.Wait()

	scanner := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	lines := 0
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode concurrent JSONL line: %v line=%q", err, scanner.Text())
		}
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan concurrent JSONL: %v", err)
	}
	if lines != count {
		t.Fatalf("unexpected JSONL line count: got %d, want %d", lines, count)
	}
}

func TestJSONLSinkValidatesContextRecordsAndWrites(t *testing.T) {
	if sink, err := NewJSONLSink(nil); err == nil || sink != nil {
		t.Fatalf("unexpected nil writer result: sink=%#v err=%v", sink, err)
	}
	var sink *JSONLSink
	if err := sink.Write(context.Background(), validAuditRecord()); err == nil {
		t.Fatal("nil JSONL sink did not fail")
	}
	validSink, err := NewJSONLSink(io.Discard)
	if err != nil {
		t.Fatalf("create discard sink: %v", err)
	}
	if err := validSink.Write(nil, validAuditRecord()); err == nil {
		t.Fatal("nil context did not fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validSink.Write(ctx, validAuditRecord()); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected canceled write error: %v", err)
	}
	invalid := validAuditRecord()
	invalid.ArgumentsSHA256 = "invalid"
	if err := validSink.Write(context.Background(), invalid); err == nil {
		t.Fatal("invalid audit record did not fail")
	}

	expected := errors.New("disk failed")
	failing, err := NewJSONLSink(auditErrorWriter{err: expected})
	if err != nil {
		t.Fatalf("create failing sink: %v", err)
	}
	if err := failing.Write(context.Background(), validAuditRecord()); !errors.Is(err, expected) {
		t.Fatalf("unexpected writer error: %v", err)
	}
	short, err := NewJSONLSink(shortAuditWriter{})
	if err != nil {
		t.Fatalf("create short writer sink: %v", err)
	}
	if err := short.Write(context.Background(), validAuditRecord()); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("unexpected short write error: %v", err)
	}
}

func TestMemorySinkSnapshotsAndPropagatesErrors(t *testing.T) {
	sink := NewMemorySink()
	record := validAuditRecord()
	if err := sink.Write(context.Background(), record); err != nil {
		t.Fatalf("write memory audit: %v", err)
	}
	snapshot := sink.Snapshot()
	if len(snapshot) != 1 || snapshot[0] != record {
		t.Fatalf("unexpected memory snapshot: %#v", snapshot)
	}
	snapshot[0].Reason = "changed"
	if sink.Snapshot()[0].Reason == "changed" {
		t.Fatal("memory snapshot shares record storage")
	}
	expected := errors.New("audit unavailable")
	sink.SetError(expected)
	if err := sink.Write(context.Background(), record); !errors.Is(err, expected) {
		t.Fatalf("unexpected memory sink error: %v", err)
	}
}

func validAuditRecord() Record {
	return Record{
		Timestamp: time.Unix(1_700_000_000, 0).UTC(), SessionID: "session-1", RequestID: "request-1",
		ToolName: "write_file", ArgumentsSHA256: strings.Repeat("a", 64), Risk: "high",
		Outcome: OutcomeAllow, Scope: "once", Source: "user", Reason: "approved", DurationMS: 42,
	}
}

type auditErrorWriter struct{ err error }

func (writer auditErrorWriter) Write([]byte) (int, error) { return 0, writer.err }

type shortAuditWriter struct{}

func (shortAuditWriter) Write(content []byte) (int, error) { return len(content) - 1, nil }
