package protocol

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestCompletedItemResumePreservesToolResultMetadata(t *testing.T) {
	now := time.Now().UTC()
	item := TurnItem{
		ID: "call-1", Kind: ItemCommandExecution, Status: ItemStatusCompleted,
		CreatedAt: now, CompletedAt: now, ToolName: "execute_command", CallID: "call-1",
		Text: "script output", ToolResult: &tool.ToolResult{
			CallID: "call-1", ToolName: "execute_command", Text: "script output",
			Metadata: map[string]any{"skill_name": "review", "skill_script": "scripts/check.sh", "skill_revision": "rev-1"},
		},
		Payload: map[string]any{"action_summary": "ran script"},
	}
	stored, err := NewCompletedItem(item)
	if err != nil {
		t.Fatal(err)
	}
	line := rollout.Line{Item: stored}
	resumed, err := DecodeCompletedItem(line)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ToolResult == nil || resumed.ToolResult.Metadata["skill_name"] != "review" || resumed.ToolResult.Metadata["skill_script"] != "scripts/check.sh" {
		t.Fatalf("ToolResult metadata was lost during resume: %#v", resumed)
	}
}
