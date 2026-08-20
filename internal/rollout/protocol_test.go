package rollout

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestRolloutItemVariantsRoundTrip(t *testing.T) {
	now := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	result := tool.ToolResult{CallID: "call-1", ToolName: "read", Text: "contents", Partial: true, Metadata: map[string]any{"path": "README.md"}}
	tests := []struct {
		name string
		item RolloutItem
	}{
		{name: "session meta", item: SessionMetaItem{ThreadID: "thread-1", CWD: "/workspace", Title: "Inspect", ModelProvider: "mock", Model: "model", CreatedAt: now}},
		{name: "response", item: ResponseItem{
			ThreadID: "thread-1", TurnID: "turn-1", Type: ResponseToolResult, Role: "tool",
			CallID: "call-1", Name: "read", Status: "succeeded", Content: "contents", Result: &result,
			Metadata: map[string]any{"path": "README.md"}, Partial: true, Duration: int64(time.Second),
		}},
		{name: "compacted", item: CompactedItem{
			ThreadID: "thread-1", TurnID: "turn-1", Summary: "inspection completed",
			ReplacementHistory:     []ReplacementMessage{{Role: "user", Content: "inspect"}, {Role: "assistant", Content: "summary"}},
			CoveredThroughSequence: 12, SourceHash: "source-hash", Provider: "mock", Model: "model",
		}},
		{name: "turn context", item: TurnContextItem{
			ThreadID: "thread-1", TurnID: "turn-1", Provider: "mock", Model: "model", CWD: "/workspace",
			Shell: "bash", CurrentDate: "2026-08-20", Timezone: "Asia/Shanghai", Mode: "default",
		}},
		{name: "event message", item: EventMsgItem{Msg: protocol.TokenCountEvent{
			ThreadID: "thread-1", TurnID: "turn-1", Usage: llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected := Line{Version: CurrentVersion, Sequence: 1, Timestamp: now, Item: test.item}
			encoded, err := json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			var actual Line
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("round trip:\nactual:   %#v\nexpected: %#v", actual, expected)
			}
		})
	}
}

func TestResponseItemValidationRejectsInvalidContracts(t *testing.T) {
	tests := []ResponseItem{
		{ThreadID: "thread-1", TurnID: "turn-1", Type: "future"},
		{ThreadID: "thread-1", TurnID: "turn-1", Type: ResponseToolCall, CallID: "call-1", Name: "read"},
		{ThreadID: "thread-1", TurnID: "turn-1", Type: ResponseToolResult, CallID: "call-1", Name: "read", Status: "succeeded"},
	}
	for _, item := range tests {
		if err := item.Validate(); err == nil {
			t.Fatalf("invalid response item was accepted: %#v", item)
		}
	}
}

func TestLineRejectsUnsupportedFormats(t *testing.T) {
	now := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC).Format(time.RFC3339Nano)
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "version one", content: `{"version":1,"sequence":1,"timestamp":"` + now + `","type":"session_meta","payload":{}}`, want: "unsupported rollout format version 1"},
		{name: "unknown type", content: `{"version":2,"sequence":1,"timestamp":"` + now + `","type":"future_item","payload":{}}`, want: `unsupported rollout item type "future_item"`},
		{name: "legacy nested item", content: `{"version":2,"sequence":1,"timestamp":"` + now + `","item":{"kind":"response_item","payload":{}}}`, want: "unsupported rollout item format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var line Line
			err := json.Unmarshal([]byte(test.content), &line)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLineValidationRejectsSequenceAndThreadMismatch(t *testing.T) {
	line := Line{
		Version: CurrentVersion, Sequence: 2, Timestamp: time.Now().UTC(),
		Item: ResponseItem{ThreadID: "thread-1", TurnID: "turn-1", Type: ResponseUserMessage, Role: "user", Content: "hello"},
	}
	if err := line.Validate("thread-1", 1); err == nil || !strings.Contains(err.Error(), "expected 1") {
		t.Fatalf("sequence mismatch error = %v", err)
	}
	if err := line.Validate("thread-2", 2); err == nil || !strings.Contains(err.Error(), `expected "thread-2"`) {
		t.Fatalf("thread mismatch error = %v", err)
	}
}
