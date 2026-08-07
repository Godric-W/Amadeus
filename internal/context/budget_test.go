package agentcontext

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestCompactConversationPreservesCompleteToolProtocolGroup(t *testing.T) {
	messages := []llm.Message{
		llm.UserMessage("old request " + strings.Repeat("history ", 500)),
		llm.AssistantMessage("old response " + strings.Repeat("detail ", 500)),
		llm.UserMessage("inspect README"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
		llm.ToolResultMessage("call-1", `{"ok":true,"text":"contents"}`),
		llm.AssistantMessage("README contains contents"),
	}
	sequences := []int64{1, 2, 3, 4, 5, 6}

	kept, compaction, err := CompactConversationWithSources(messages, sequences, 300, ConservativeEstimator{})
	if err != nil {
		t.Fatal(err)
	}
	if compaction == nil || compaction.CoveredMessages != 2 || compaction.CoveredThroughSequence != 2 {
		t.Fatalf("unexpected compaction boundary: %#v", compaction)
	}
	if len(kept) != 4 || kept[0].Role != llm.RoleUser || len(kept[1].ToolCalls) != 1 || kept[2].Role != llm.RoleTool || kept[3].Role != llm.RoleAssistant {
		t.Fatalf("Tool protocol group was split: %#v", kept)
	}
}

func TestAtomicConversationGroupsMergeEqualSourceSequenceBoundaries(t *testing.T) {
	messages := []llm.Message{
		llm.UserMessage("first"),
		llm.UserMessage("replacement goal"),
		llm.UserMessage("replacement state"),
		llm.UserMessage("tail"),
	}
	groups, err := atomicConversationGroups(messages, []int64{1, 2, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || groups[1].start != 1 || groups[1].end != 3 {
		t.Fatalf("equal source sequence was split across groups: %#v", groups)
	}
}

func TestCompactConversationRejectsIncompleteToolProtocolGroup(t *testing.T) {
	messages := []llm.Message{
		llm.UserMessage(strings.Repeat("old ", 500)),
		llm.UserMessage("current"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
	}
	if _, _, err := CompactConversation(messages, 100, ConservativeEstimator{}); err == nil || !strings.Contains(err.Error(), "incomplete Tool Call group") {
		t.Fatalf("expected incomplete protocol error, got %v", err)
	}
}
