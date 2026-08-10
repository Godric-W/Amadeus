package agentcontext

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestContextWindowManagerPreservesPinnedAndCurrentMessagesWhileDroppingPairs(t *testing.T) {
	manager := NewContextWindowManager(ConservativeEstimator{})
	base := BaseEnvelope{Messages: []llm.Message{
		llm.SystemMessage("system protocol"), llm.DeveloperMessage("project instructions"),
		llm.UserMessage(strings.Repeat("old request ", 80)), llm.AssistantMessage(strings.Repeat("old response ", 80)),
		llm.UserMessage(strings.Repeat("recent request ", 30)), llm.AssistantMessage(strings.Repeat("recent response ", 30)),
		llm.UserMessage("current goal"),
	}}
	view, err := manager.Prepare(context.Background(), WindowRequest{Base: base, Profile: DefaultContextProfile(900)})
	if err != nil {
		t.Fatal(err)
	}
	if view.Compaction == nil || view.Compaction.DroppedMessagePairs == 0 {
		t.Fatalf("expected complete-pair compaction: %#v", view)
	}
	if view.Messages[0].Role != llm.RoleSystem || view.Messages[1].Role != llm.RoleDeveloper || view.Messages[len(view.Messages)-1].Content != "current goal" {
		t.Fatalf("pinned/current messages changed: %#v", view.Messages)
	}
	for index := 0; index < len(view.Messages); index++ {
		if view.Messages[index].Role == llm.RoleUser && index+1 < len(view.Messages) && view.Messages[index+1].Role == llm.RoleAssistant {
			index++
		}
	}
}

func TestContextWindowManagerProjectsToolResultAndPreservesMetadata(t *testing.T) {
	manager := NewContextWindowManager(ConservativeEstimator{})
	payload, err := json.Marshal(map[string]any{
		"ok": false, "text": strings.Repeat("command output ", 2000), "partial": true,
		"error": "exit status 1", "metadata": map[string]any{"exit_code": 1, "truncated": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	assistant := llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "execute_command", Arguments: json.RawMessage(`{"command":"go test ./..."}`)})
	view, err := manager.Prepare(context.Background(), WindowRequest{
		Base:    BaseEnvelope{Messages: []llm.Message{llm.SystemMessage("system"), llm.UserMessage("run tests")}},
		Runtime: []llm.Message{assistant, llm.ToolResultMessage("call-1", string(payload))},
		Profile: DefaultContextProfile(4000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Compaction == nil || view.Compaction.ProjectedToolResults != 1 {
		t.Fatalf("tool result was not projected: %#v", view.Compaction)
	}
	var projected map[string]any
	if err := json.Unmarshal([]byte(view.Messages[len(view.Messages)-1].Content), &projected); err != nil {
		t.Fatalf("projected tool result is invalid JSON: %v", err)
	}
	metadata := projected["metadata"].(map[string]any)
	if projected["error"] != "exit status 1" || projected["partial"] != true || metadata["exit_code"] != float64(1) || projected["context_truncated"] != true {
		t.Fatalf("tool result metadata was lost: %#v", projected)
	}
}

func TestContextWindowManagerAccountsForToolsAndProviderUsage(t *testing.T) {
	manager := NewContextWindowManager(nil)
	usage := llm.Usage{InputTokens: 321}
	view, err := manager.Prepare(context.Background(), WindowRequest{
		Base:    BaseEnvelope{Messages: []llm.Message{llm.SystemMessage("system"), llm.UserMessage("goal")}, AvailableTools: []tool.Spec{{Name: "read_file", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
		Profile: DefaultContextProfile(8000), PreviousUsage: &usage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Usage.ToolTokens <= 0 || view.Usage.ProviderInputTokens != 321 || len(view.SHA256) != 64 {
		t.Fatalf("unexpected context usage/hash: %#v", view)
	}
}

func TestDefaultContextProfileMatchesCodexWindowSemantics(t *testing.T) {
	profile := DefaultContextProfile(128_000)
	if profile.EffectiveContextWindowPercent != 95 || profile.EffectiveInputLimit() != 121_600 || profile.AutoCompactTokenLimit != 115_200 {
		t.Fatalf("unexpected default context profile: %#v", profile)
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("validate default context profile: %v", err)
	}
}

func TestContextWindowManagerProjectsHistoricalToolResults(t *testing.T) {
	manager := NewContextWindowManager(ConservativeEstimator{})
	payload, err := json.Marshal(map[string]any{"ok": true, "text": strings.Repeat("historical output ", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	base := BaseEnvelope{Messages: []llm.Message{
		llm.SystemMessage("system"),
		llm.UserMessage("inspect"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-history", Name: "read_file", Arguments: json.RawMessage(`{"path":"docs/design.md"}`)}),
		llm.ToolResultMessage("call-history", string(payload)),
		llm.UserMessage("continue"),
	}}
	view, err := manager.Prepare(context.Background(), WindowRequest{Base: base, Profile: DefaultContextProfile(8_000)})
	if err != nil {
		t.Fatal(err)
	}
	if view.Compaction == nil || view.Compaction.ProjectedToolResults != 1 {
		t.Fatalf("historical tool result was not projected: %#v", view.Compaction)
	}
	if !strings.Contains(view.Messages[3].Content, "context_truncated") {
		t.Fatalf("historical tool projection marker missing: %q", view.Messages[3].Content)
	}
}
