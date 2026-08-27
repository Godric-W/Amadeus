package prompt

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func TestLoadModelMessagesSeparatesModelModesAndCompaction(t *testing.T) {
	messages, err := LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	base, err := messages.ResolveBaseInstructions("", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(base.Text) == "" || base.Provenance.Type != llm.BaseInstructionsModel || base.Provenance.Model != "test-model" {
		t.Fatalf("base instructions are incomplete: %#v", base)
	}
	if strings.TrimSpace(messages.CollaborationModes.Default) == "" || strings.TrimSpace(messages.CollaborationModes.Plan) == "" {
		t.Fatalf("collaboration mode instructions are incomplete: %#v", messages.CollaborationModes)
	}
	if strings.TrimSpace(messages.MultiAgent.Role.Subagent) == "" {
		t.Fatalf("sub-agent developer instructions are incomplete: %q", messages.MultiAgent.Role.Subagent)
	}
	compaction, err := LoadCompactionAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !compaction.Valid() {
		t.Fatalf("compaction assets are incomplete: %#v", compaction)
	}
	if compaction.SummarizationRevision == compaction.SummaryPrefixRevision {
		t.Fatal("compaction prompt and summary prefix share one revision")
	}
}

func TestSubagentInstructionsDoNotAppendToolGuidance(t *testing.T) {
	messages := mustModelMessages(t)
	text, err := RenderSubagentRoleInstructions(messages)
	if err != nil {
		t.Fatal(err)
	}
	if text != strings.TrimSpace(messages.MultiAgent.Role.Subagent) || strings.Contains(text, "## `read`") {
		t.Fatalf("sub-agent instructions have a second Tool guidance owner: %q", text)
	}
}

func TestCollaborationInstructionsDoNotAppendToolGuidance(t *testing.T) {
	messages := mustModelMessages(t)
	text, err := RenderCollaborationInstructions(messages, protocol.ModeKindDefault)
	if err != nil {
		t.Fatal(err)
	}
	if text != strings.TrimSpace(messages.CollaborationModes.Default) || strings.Contains(text, "## `update_plan`") {
		t.Fatalf("Default collaboration instructions have a second Tool guidance owner: %q", text)
	}
	plan, err := RenderCollaborationInstructions(messages, protocol.ModeKindPlan)
	if err != nil {
		t.Fatal(err)
	}
	if plan != strings.TrimSpace(messages.CollaborationModes.Plan) || strings.Count(plan, "# Plan Mode (Conversational)") != 1 || strings.Contains(plan, "## `read`") {
		t.Fatalf("Plan collaboration instructions are incorrect: %q", plan)
	}
}

func mustModelMessages(t *testing.T) llm.ModelMessages {
	t.Helper()
	messages, err := LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	return messages
}
