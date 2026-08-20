package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestCanonicalProjectionIsEquivalentForLiveAndResume(t *testing.T) {
	result := tool.ToolResult{
		CallID: "call-1", ToolName: "execute_command", Text: strings.Repeat("output", 600), Partial: true,
		Parts:    []tool.ContentPart{{Kind: tool.ContentText, Text: "stderr"}, {Kind: tool.ContentImage, MediaType: "image/png", Data: "aW1hZ2U="}},
		Metadata: map[string]any{"exit_code": 7, "output_truncated": true, "secret": "do-not-project"},
	}
	lines := []rollout.Line{
		contextResponseLine(t, 1, rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "run tests"}),
		contextResponseLine(t, 2, rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "execute_command", Arguments: json.RawMessage(`{"command":"go test ./..."}`)}),
		contextResponseLine(t, 3, rollout.ResponseItem{
			Type: rollout.ResponseToolResult, Role: "tool", CallID: "call-1", Name: "execute_command", Status: "failed", Result: &result,
			Error: &rollout.ResponseError{Kind: "process_error", Message: "exit status 7"}, Partial: true,
		}),
		contextEventLine(t, 4, protocol.TokenCountEvent{Usage: llm.Usage{InputTokens: 20, OutputTokens: 5, TotalTokens: 25}}),
		contextEventLine(t, 5, protocol.TokenCountEvent{Usage: llm.Usage{InputTokens: 4, OutputTokens: 1, TotalTokens: 5}}),
	}
	model := llm.ModelInfo{ContextWindow: 10_000, ToolOutputTokenLimit: 160, InputModalities: []llm.InputModality{llm.InputModalityText}}
	live := NewManager(nil)
	for _, line := range lines {
		if err := live.Record(line.Sequence, line.Item); err != nil {
			t.Fatal(err)
		}
	}
	resumed, err := NewManagerFromRollout(lines, nil)
	if err != nil {
		t.Fatal(err)
	}
	liveSnapshot := live.Snapshot(model, llm.Prompt{})
	resumeSnapshot := resumed.Snapshot(model, llm.Prompt{})
	if !reflect.DeepEqual(liveSnapshot.Items, resumeSnapshot.Items) || !reflect.DeepEqual(liveSnapshot.Usage, resumeSnapshot.Usage) {
		t.Fatalf("live projection %#v differs from resume %#v", liveSnapshot, resumeSnapshot)
	}
	if liveSnapshot.Usage.ProviderUsage.TotalTokens != 30 {
		t.Fatalf("cumulative usage = %#v", liveSnapshot.Usage.ProviderUsage)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal([]byte(liveSnapshot.Items[2].Content), &payload); err != nil {
		t.Fatalf("structured result was corrupted by truncation: %v", err)
	}
	if payload.Status != "failed" || payload.Error == nil || payload.Error.Kind != "process_error" || !payload.Partial || !payload.Truncated || payload.Metadata["exit_code"] == nil || payload.Metadata["secret"] != nil {
		t.Fatalf("tool result semantics = %#v", payload)
	}
	if len(liveSnapshot.Items[2].Parts) != 1 || liveSnapshot.Items[2].Parts[0].Kind != llm.ContentText || !reflect.DeepEqual(payload.Omitted, []string{"image"}) {
		t.Fatalf("text-only modality projection = item %#v payload %#v", liveSnapshot.Items[2], payload)
	}
}

func TestIncrementalRecordMatchesResumeAcrossTerminalAndCompactionFacts(t *testing.T) {
	toolResult := tool.ToolResult{CallID: "call-1", ToolName: "read", Text: "package main", Metadata: map[string]any{"path": "main.go"}}
	items := []rollout.RolloutItem{
		mustContextResponseItem(t, rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "inspect main.go"}),
		mustContextResponseItem(t, rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "I will inspect it."}),
		mustContextResponseItem(t, rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)}),
		mustContextResponseItem(t, rollout.ResponseItem{Type: rollout.ResponseToolResult, Role: "tool", CallID: "call-1", Name: "read", Status: "succeeded", Result: &toolResult}),
		rollout.EventMsgItem{Msg: protocol.TokenCountEvent{Usage: llm.Usage{InputTokens: 12, OutputTokens: 3, TotalTokens: 15}}},
		rollout.EventMsgItem{Msg: protocol.ContextUpdateEvent{Key: string(UpdateAgents), Content: "project agents", Revision: "agents-r1"}},
		rollout.EventMsgItem{Msg: protocol.TurnAbortedEvent{Reason: "interrupted", FinishedAt: time.Unix(7, 0).UTC()}},
	}
	for index := range items {
		items[index] = rollout.ScopeItem(items[index], "thread-1", "turn-1")
	}

	live := NewManager(nil)
	if err := live.Record(1, items[:4]...); err != nil {
		t.Fatal(err)
	}
	if err := live.Record(5, items[4:]...); err != nil {
		t.Fatal(err)
	}
	covered := live.Projection()
	encoded, err := json.Marshal(covered.Messages)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	compacted := rollout.ScopeItem(rollout.CompactedItem{
		Summary: "inspection checkpoint",
		ReplacementHistory: []rollout.ReplacementMessage{
			{Role: "user", Content: "inspect main.go"},
			{Role: "assistant", Content: "Inspection was interrupted after reading main.go."},
		},
		CoveredThroughSequence: 7,
		SourceHash:             hex.EncodeToString(digest[:]),
	}, "thread-1", "turn-2")
	trailing := rollout.ScopeItem(rollout.EventMsgItem{Msg: protocol.TokenCountEvent{Usage: llm.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}}, "thread-1", "turn-2")
	if err := live.Record(8, compacted, trailing); err != nil {
		t.Fatal(err)
	}

	allItems := append(append([]rollout.RolloutItem(nil), items...), compacted, trailing)
	lines := make([]rollout.Line, len(allItems))
	for index, item := range allItems {
		lines[index] = rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(index + 1), Timestamp: time.Unix(int64(index+1), 0).UTC(), Item: item}
	}
	resumed, err := NewManagerFromRollout(lines, nil)
	if err != nil {
		t.Fatal(err)
	}

	model := llm.ModelInfo{ContextWindow: 10_000}
	if live.NextSequence() != resumed.NextSequence() || live.RolloutItemCount() != resumed.RolloutItemCount() {
		t.Fatalf("sequence state differs: live next/count=%d/%d resume=%d/%d", live.NextSequence(), live.RolloutItemCount(), resumed.NextSequence(), resumed.RolloutItemCount())
	}
	if !reflect.DeepEqual(live.Projection(), resumed.Projection()) {
		t.Fatalf("projection differs: live=%#v resume=%#v", live.Projection(), resumed.Projection())
	}
	if !reflect.DeepEqual(live.Snapshot(model, llm.Prompt{}), resumed.Snapshot(model, llm.Prompt{})) {
		t.Fatalf("prompt snapshot differs: live=%#v resume=%#v", live.Snapshot(model, llm.Prompt{}), resumed.Snapshot(model, llm.Prompt{}))
	}
	if live.Update(UpdateAgents) != "project agents" || resumed.Update(UpdateAgents) != "project agents" {
		t.Fatalf("context update differs: live=%q resume=%q", live.Update(UpdateAgents), resumed.Update(UpdateAgents))
	}
	if usage := live.Snapshot(model, llm.Prompt{}).Usage.ProviderUsage; usage.TotalTokens != 18 {
		t.Fatalf("cumulative usage = %#v", usage)
	}
	projection := live.Projection()
	if len(projection.Messages) != 2 || projection.Messages[1].Content != "Inspection was interrupted after reading main.go." {
		t.Fatalf("compaction replacement = %#v", projection.Messages)
	}
}

func mustContextResponseItem(t *testing.T, item rollout.ResponseItem) rollout.ResponseItem {
	t.Helper()
	value, err := rollout.NewResponseItem(item)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestImageCapableModelPreservesToolImageParts(t *testing.T) {
	message, err := ProjectToolResult(ToolResultProjection{
		CallID: "call-image", Status: "succeeded",
		Result: tool.ToolResult{Parts: []tool.ContentPart{{Kind: tool.ContentImage, MediaType: "image/png", Data: "aW1hZ2U="}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items := NormalizeResponseItems([]llm.ResponseItem{
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-image", Name: "view_image", Arguments: json.RawMessage(`{"path":"image.png"}`)}),
		message,
	}, llm.ModelInfo{InputModalities: []llm.InputModality{llm.InputModalityText, llm.InputModalityImage}}, nil)
	if len(items) != 2 || len(items[1].Parts) != 1 || items[1].Parts[0].Kind != llm.ContentImage {
		t.Fatalf("image-capable projection = %#v", items)
	}
}
