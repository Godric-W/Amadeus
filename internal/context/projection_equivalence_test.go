package agentcontext

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

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
	for index := range lines {
		if err := live.Rebuild(lines[:index+1]); err != nil {
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
