package rollout

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestResponseItemRoundTrip(t *testing.T) {
	tests := []ResponseItem{
		{Type: ResponseUserMessage, Role: "user", Content: "inspect project"},
		{Type: ResponseAssistantMessage, Role: "assistant", Content: "I will inspect it.", Reasoning: "Need repository context."},
		{Type: ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)},
		{
			Type: ResponseToolResult, Role: "tool", CallID: "call-1", Name: "read", Status: "succeeded",
			Content: "contents", Partial: true, Duration: int64(time.Second), Metadata: map[string]any{"path": "README.md"},
			Result: &tool.ToolResult{CallID: "call-1", ToolName: "read", Text: "contents", Partial: true, Metadata: map[string]any{"path": "README.md"}},
		},
	}
	for _, expected := range tests {
		item, err := NewResponseItem(expected)
		if err != nil {
			t.Fatalf("create %s: %v", expected.Type, err)
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var decodedItem Item
		if err := json.Unmarshal(encoded, &decodedItem); err != nil {
			t.Fatal(err)
		}
		if err := decodedItem.Validate(); err != nil {
			t.Fatalf("validate %s after round trip: %v", expected.Type, err)
		}
		actual, err := DecodeResponseItem(decodedItem)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s round trip:\nactual:   %#v\nexpected: %#v", expected.Type, actual, expected)
		}
	}
}

func TestCompactionAndTerminalRoundTrip(t *testing.T) {
	compaction := Compaction{
		Summary: "inspection completed",
		ReplacementHistory: []ReplacementMessage{
			{Role: "user", Content: "inspect"},
			{Role: "assistant", Content: "## Compaction Checkpoint\n\ninspection completed"},
		},
		CoveredThroughSequence: 12, SourceHash: "source-hash", Provider: "mock", Model: "model",
	}
	assertPayloadRoundTrip(t, KindCompaction, compaction)
	assertPayloadRoundTrip(t, KindTurnCompleted, TurnCompleted{Status: TurnStatusCompleted, Summary: "done"})
	assertPayloadRoundTrip(t, KindTurnAborted, TurnAborted{Summary: "cancelled", Reason: "interrupt"})
}

func TestTypedPayloadValidationRejectsInvalidContracts(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		payload any
	}{
		{name: "unknown response type", kind: KindResponseItem, payload: ResponseItem{Type: "future"}},
		{name: "incomplete tool call", kind: KindResponseItem, payload: ResponseItem{Type: ResponseToolCall, CallID: "call-1", Name: "read"}},
		{name: "incomplete tool result", kind: KindResponseItem, payload: ResponseItem{Type: ResponseToolResult, CallID: "call-1", Name: "read", Status: "succeeded"}},
		{name: "incomplete compaction", kind: KindCompaction, payload: Compaction{Summary: "summary"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewItem(test.kind, test.payload); err == nil {
				t.Fatal("invalid typed payload was accepted")
			}
		})
	}
}

func TestLineRejectsUnknownVersion(t *testing.T) {
	item, err := NewResponseItem(ResponseItem{Type: ResponseUserMessage, Role: "user", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	line := Line{Version: CurrentVersion + 1, Sequence: 1, Timestamp: time.Now().UTC(), ThreadID: "thread-1", TurnID: "turn-1", Item: item}
	if err := line.Validate("thread-1", 1); err == nil {
		t.Fatal("unknown rollout version was accepted")
	}
}

func assertPayloadRoundTrip[T any](t *testing.T, kind Kind, expected T) {
	t.Helper()
	item, err := NewItem(kind, expected)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var decodedItem Item
	if err := json.Unmarshal(encoded, &decodedItem); err != nil {
		t.Fatal(err)
	}
	if err := decodedItem.Validate(); err != nil {
		t.Fatal(err)
	}
	actual, err := DecodePayload[T](decodedItem)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("round trip:\nactual:   %#v\nexpected: %#v", actual, expected)
	}
}
