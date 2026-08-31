package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTurnItemPayloadRoundTripsAsTypedVariant(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		item TurnItem
		want any
	}{
		{
			name: "tool call",
			item: TurnItem{ID: "tool-1", Kind: ItemToolCall, Status: ItemInProgress, CreatedAt: now, ToolName: "grep", CallID: "call-1", Payload: ToolCallItemPayload{ActionSummary: "Search source", SideEffect: "read"}},
			want: ToolCallItemPayload{},
		},
		{
			name: "command",
			item: TurnItem{ID: "command-1", Kind: ItemCommandExecution, Status: ItemStatusCompleted, CreatedAt: now, CompletedAt: now.Add(time.Second), ToolName: "execute_command", CallID: "call-2", Payload: CommandExecutionItemPayload{ActionSummary: "Ran tests", DurationMS: 1000}},
			want: CommandExecutionItemPayload{},
		},
		{
			name: "file change",
			item: TurnItem{ID: "file-1", Kind: ItemFileChange, Status: ItemStatusCompleted, CreatedAt: now, CompletedAt: now.Add(time.Second), ToolName: "write", CallID: "call-3", Payload: FileChangeItemPayload{ActionSummary: "Create file", SideEffect: "write"}},
			want: FileChangeItemPayload{},
		},
		{
			name: "compaction",
			item: TurnItem{ID: "compact-1", Kind: ItemContextCompaction, Status: ItemStatusCompleted, CreatedAt: now, CompletedAt: now.Add(time.Second), Payload: ContextCompactionItem{Trigger: CompactionTriggerManual, Reason: CompactionReasonUserRequested, Phase: CompactionPhaseStandaloneTurn}},
			want: ContextCompactionItem{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.item)
			if err != nil {
				t.Fatal(err)
			}
			var decoded TurnItem
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			switch test.want.(type) {
			case ToolCallItemPayload:
				if _, ok := decoded.Payload.(ToolCallItemPayload); !ok {
					t.Fatalf("payload type = %T", decoded.Payload)
				}
			case CommandExecutionItemPayload:
				if _, ok := decoded.Payload.(CommandExecutionItemPayload); !ok {
					t.Fatalf("payload type = %T", decoded.Payload)
				}
			case FileChangeItemPayload:
				if _, ok := decoded.Payload.(FileChangeItemPayload); !ok {
					t.Fatalf("payload type = %T", decoded.Payload)
				}
			case ContextCompactionItem:
				if _, ok := decoded.Payload.(ContextCompactionItem); !ok {
					t.Fatalf("payload type = %T", decoded.Payload)
				}
			}
		})
	}
}

func TestTurnItemRejectsPayloadForWrongKind(t *testing.T) {
	item := TurnItem{
		ID: "tool-1", Kind: ItemToolCall, Status: ItemInProgress, CreatedAt: time.Now().UTC(),
		Payload: CommandExecutionItemPayload{},
	}
	if err := item.Validate(); err == nil || !strings.Contains(err.Error(), "tool call item payload type") {
		t.Fatalf("wrong payload validation error = %v", err)
	}
}

func TestTurnItemRejectsPayloadOnPayloadlessKind(t *testing.T) {
	item := TurnItem{
		ID: "plan-1", Kind: ItemPlan, Status: ItemStatusCompleted, CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(),
		Payload: ToolCallItemPayload{},
	}
	if err := item.Validate(); err == nil || !strings.Contains(err.Error(), "does not accept a payload") {
		t.Fatalf("payloadless kind validation error = %v", err)
	}
}

func TestTurnItemRejectsMissingTypedPayload(t *testing.T) {
	item := TurnItem{ID: "tool-1", Kind: ItemToolCall, Status: ItemInProgress, CreatedAt: time.Now().UTC(), ToolName: "read", CallID: "call-1"}
	if err := item.Validate(); err == nil || !strings.Contains(err.Error(), "item payload is missing") {
		t.Fatalf("missing typed payload validation error = %v", err)
	}
	encoded := []byte(`{"id":"tool-1","kind":"tool_call","status":"in_progress","created_at":"2026-08-31T12:00:00Z","tool_name":"read","call_id":"call-1"}`)
	var decoded TurnItem
	if err := json.Unmarshal(encoded, &decoded); err == nil || !strings.Contains(err.Error(), "item payload is missing") {
		t.Fatalf("missing wire payload decode error = %v", err)
	}
}
