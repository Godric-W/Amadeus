package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func TestManagerNormalizesToolProtocolAndProjectsLargeResults(t *testing.T) {
	manager := NewManager(ConservativeEstimator{})
	manager.Record(
		llm.UserMessage("inspect"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"large.md"}`)}),
		llm.ToolResultMessage("call-1", strings.Repeat("x", 5000)),
		llm.ToolResultMessage("orphan", "must disappear"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-2", Name: "execute_command", Arguments: json.RawMessage(`{"command":"long"}`)}),
	)
	snapshot := manager.ForPrompt(llm.ModelInfo{ContextWindow: 10000, ToolOutputMaxTokens: 100})
	if len(snapshot.Items) != 5 {
		t.Fatalf("unexpected normalized item count: %#v", snapshot.Items)
	}
	if snapshot.Items[2].Role != llm.RoleTool || !strings.Contains(snapshot.Items[2].Content, "truncated") {
		t.Fatalf("large tool output was not projected: %#v", snapshot.Items[2])
	}
	if snapshot.Items[4].Role != llm.RoleTool || snapshot.Items[4].ToolCallID != "call-2" {
		t.Fatalf("missing tool result was not synthesized: %#v", snapshot.Items[4])
	}
	for _, item := range snapshot.Items {
		if strings.Contains(item.Content, "must disappear") {
			t.Fatal("orphan tool result leaked into prompt")
		}
	}
}

func TestManagerDynamicUpdatesAreStableAndOrdered(t *testing.T) {
	manager := NewManager(nil)
	manager.Record(llm.UserMessage("hello"))
	manager.ReplaceUpdate(UpdateMCP, "mcp")
	manager.ReplaceUpdate(UpdateDeveloperInstructions, "developer")
	manager.ReplaceUpdate(UpdateAgents, "agents")
	first := manager.ForPrompt(llm.ModelInfo{ContextWindow: 1000})
	if first.Items[0].Content != "developer" || first.Items[1].Content != "agents" || first.Items[2].Content != "mcp" {
		t.Fatalf("dynamic context order is unstable: %#v", first.Items)
	}
	version := manager.HistoryVersion()
	if manager.ReplaceUpdate(UpdateAgents, "agents") || manager.HistoryVersion() != version {
		t.Fatal("unchanged dynamic update changed history version")
	}
}

func TestManagerSeparatesProviderAndEstimatedUsage(t *testing.T) {
	manager := NewManager(nil)
	manager.Record(llm.UserMessage("hello"))
	manager.UpdateUsage(llm.Usage{InputTokens: 17, OutputTokens: 5, TotalTokens: 22})
	snapshot := manager.ForPrompt(llm.ModelInfo{ContextWindow: 1000})
	if !snapshot.Usage.HasProviderUsage || snapshot.Usage.ProviderUsage.TotalTokens != 22 {
		t.Fatalf("provider usage was not retained: %#v", snapshot.Usage)
	}
	if snapshot.Usage.EstimatedInputTokens <= 0 {
		t.Fatalf("estimated usage was not calculated: %#v", snapshot.Usage)
	}
	if snapshot.Usage.EstimatedInputTokens == snapshot.Usage.ProviderUsage.InputTokens {
		t.Fatal("provider and estimated usage were conflated")
	}
}

func TestManagerRebuildRestoresCanonicalProjectionAndClearsStaleState(t *testing.T) {
	manager := NewManager(nil)
	manager.Record(llm.UserMessage("stale history"))
	manager.ReplaceUpdate(UpdateMCP, "stale mcp")
	manager.UpdateUsage(llm.Usage{TotalTokens: 999})

	covered := []llm.Message{
		llm.UserMessage("initial objective"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
		llm.ToolResultMessage("call-1", "full contents"),
		llm.AssistantMessage("inspection complete"),
	}
	encoded, err := json.Marshal(covered)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	lines := []rollout.Line{
		contextTestLine(t, 1, rollout.KindResponseItem, map[string]any{"type": "user_message", "role": "user", "content": "initial objective"}),
		contextTestLine(t, 2, rollout.KindResponseItem, map[string]any{"type": "tool_call", "role": "assistant", "call_id": "call-1", "name": "read", "arguments": map[string]any{"path": "README.md"}}),
		contextTestLine(t, 3, rollout.KindResponseItem, map[string]any{"type": "tool_result", "role": "tool", "call_id": "call-1", "content": "full contents"}),
		contextTestLine(t, 4, rollout.KindResponseItem, map[string]any{"type": "assistant_message", "role": "assistant", "content": "inspection complete"}),
		contextTestLine(t, 5, rollout.KindContextUpdate, rollout.ContextUpdate{Key: string(UpdateAgents), Content: "project agents"}),
		contextTestLine(t, 6, rollout.KindCompaction, map[string]any{
			"covered_through_sequence": 4,
			"source_hash":              hex.EncodeToString(digest[:]),
			"replacement_history": []map[string]any{
				{"role": "user", "content": "initial objective"},
				{"role": "assistant", "content": "## Compaction Checkpoint\n\ninspection complete"},
			},
		}),
		contextTestLine(t, 7, rollout.KindResponseItem, map[string]any{"type": "user_message", "role": "user", "content": "now run tests"}),
		contextTestLine(t, 8, rollout.KindResponseItem, map[string]any{"type": "tool_call", "role": "assistant", "call_id": "call-2", "name": "execute_command", "arguments": map[string]any{"command": "go test ./..."}}),
		contextTestLine(t, 9, rollout.KindTurnAborted, rollout.TurnAborted{Reason: "interrupted"}),
		contextTestLine(t, 10, rollout.KindTokenUsage, rollout.TokenUsage{InputTokens: 40, OutputTokens: 8, TotalTokens: 48}),
	}
	if err := manager.Rebuild(lines); err != nil {
		t.Fatal(err)
	}
	snapshot := manager.ForPrompt(llm.ModelInfo{ContextWindow: 10_000})
	if len(snapshot.Items) != 7 {
		t.Fatalf("unexpected rebuilt Prompt: %#v", snapshot.Items)
	}
	if snapshot.Items[0].Role != llm.RoleDeveloper || snapshot.Items[0].Content != "project agents" {
		t.Fatalf("dynamic Context Update was not restored: %#v", snapshot.Items[0])
	}
	if snapshot.Items[1].Content != "initial objective" || !strings.Contains(snapshot.Items[2].Content, "Compaction Checkpoint") || snapshot.Items[3].Content != "now run tests" {
		t.Fatalf("replacement history was not restored: %#v", snapshot.Items)
	}
	if snapshot.Items[5].Role != llm.RoleTool || snapshot.Items[5].ToolCallID != "call-2" || !strings.Contains(snapshot.Items[5].Content, "did not complete") {
		t.Fatalf("interrupted Tool Call was not normalized: %#v", snapshot.Items)
	}
	if !snapshot.Usage.HasProviderUsage || snapshot.Usage.ProviderUsage.TotalTokens != 48 {
		t.Fatalf("provider Usage was not restored: %#v", snapshot.Usage)
	}
	for _, item := range snapshot.Items {
		if strings.Contains(item.Content, "stale") {
			t.Fatalf("stale ContextManager state survived Rebuild: %#v", snapshot.Items)
		}
	}
}

func TestManagerPromptSnapshotDoesNotShareMutableHistory(t *testing.T) {
	manager := NewManager(nil)
	manager.Record(llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}))
	first := manager.ForPrompt(llm.ModelInfo{ContextWindow: 1000})
	first.Items[0].ToolCalls[0].Name = "changed"
	first.Items[0].ToolCalls[0].Arguments[0] = '['
	second := manager.ForPrompt(llm.ModelInfo{ContextWindow: 1000})
	if second.Items[0].ToolCalls[0].Name != "read" || string(second.Items[0].ToolCalls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("Prompt snapshot shares mutable history: %#v", second.Items[0])
	}
}

func TestManagerCompactionThresholdAccountsForFullPromptAndOutputReserve(t *testing.T) {
	manager := NewManager(ConservativeEstimator{})
	manager.Record(llm.UserMessage(strings.Repeat("h", 300)))
	model := llm.ModelInfo{ContextWindow: 1000, AutoCompactTokenLimit: 900, MaxOutputTokens: 400}
	bare := llm.Prompt{}
	if manager.NeedsCompaction(model, bare) {
		t.Fatal("small history unexpectedly requires compaction")
	}
	prompt := llm.Prompt{
		BaseInstructions: llm.BaseInstructions{Text: strings.Repeat("s", 900)},
		Tools:            []llm.ToolDefinition{{Name: "large_tool", Description: strings.Repeat("d", 900), InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	if !manager.NeedsCompaction(model, prompt) {
		t.Fatal("full Prompt and output reserve were not included in compaction threshold")
	}
	if manager.EstimatePromptTokens(model, prompt) <= manager.ForPrompt(model).Usage.EstimatedInputTokens {
		t.Fatal("full Prompt estimate did not include instructions and Tool Specs")
	}
}

func contextTestLine(t *testing.T, sequence uint64, kind rollout.Kind, payload any) rollout.Line {
	t.Helper()
	item, err := rollout.NewItem(kind, payload)
	if err != nil {
		t.Fatal(err)
	}
	return rollout.Line{Version: rollout.CurrentVersion, Sequence: sequence, Timestamp: time.Unix(int64(sequence), 0).UTC(), ThreadID: "thread-1", TurnID: "turn-1", Item: item}
}
