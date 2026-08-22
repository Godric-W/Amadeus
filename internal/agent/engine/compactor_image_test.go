package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestCompactorProjectsToolImagesForEffectiveModel(t *testing.T) {
	client := &scriptedModelClient{streams: []llm.Stream{&scriptedModelStream{results: []scriptedStreamResult{
		{chunk: llm.StreamChunk{ContentDelta: "compacted"}},
		{chunk: llm.StreamChunk{FinishReason: llm.FinishReasonStop}},
	}}}}
	modelSession := newTestModelClientSession(t, client, 0, time.Second)
	modelMessages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	compactor := &Compactor{
		ProviderName: "test", ModelMessages: modelMessages,
		ModelInfo: llm.ModelInfo{Name: "test-model", ToolOutputTokenLimit: 100, InputModalities: []llm.InputModality{llm.InputModalityText}},
	}
	history := agentcontext.RolloutMessageProjection{
		Messages: []llm.ResponseItem{
			llm.UserMessage("inspect image"),
			llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-image", Name: "view_image", Arguments: json.RawMessage(`{"path":"image.png"}`)}),
			llm.ToolResultMessageWithParts("call-image", `{"ok":true,"status":"succeeded","metadata":{"prepared_width":32,"prepared_height":32},"parts":[{"kind":"image","media_type":"image/png"}]}`, llm.ImagePartWithDetail("image/png", "Y2Fub25pY2FsLWltYWdl", "high")),
			llm.AssistantMessage("image inspected"),
			llm.UserMessage("continue"),
		},
		SourceSequences: []int64{1, 2, 3, 4, 5},
	}
	if _, err := compactor.Compact(context.Background(), CompactRequest{History: history, ModelSession: modelSession, Metadata: llm.RequestMetadata{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1"}, Events: protocol.NewMemorySink()}); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("compactor requests = %d, want 1", len(client.requests))
	}
	foundToolResult := false
	for _, item := range client.requests[0].Prompt.Input {
		if item.Role != llm.RoleTool {
			continue
		}
		foundToolResult = true
		if len(item.Parts) != 0 || !strings.Contains(item.Content, `"omitted_modalities":["image"]`) {
			t.Fatalf("compactor retained unsupported image: %#v", item)
		}
	}
	if !foundToolResult {
		t.Fatalf("compactor request omitted tool result entirely: %#v", client.requests[0].Prompt.Input)
	}
}
