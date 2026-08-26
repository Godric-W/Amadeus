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
	base, err := messages.ResolveBaseInstructions("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(base.Text, "You are Amadeus") {
		t.Fatalf("base instructions are incomplete: %q", base.Text)
	}
	if !strings.Contains(messages.CollaborationModes.Default, "Execute Mode") || !strings.Contains(messages.CollaborationModes.Plan, "Plan Mode") {
		t.Fatalf("collaboration mode instructions are incomplete: %#v", messages.CollaborationModes)
	}
	if !strings.Contains(messages.SubagentDeveloperInstructions, "sub-agent spawned by another Amadeus agent") || !strings.Contains(messages.SubagentDeveloperInstructions, "Do not modify files") {
		t.Fatalf("sub-agent developer instructions are incomplete: %q", messages.SubagentDeveloperInstructions)
	}
	compaction, prefix, err := CompactionMessages(messages)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compaction.Text, "CONTEXT CHECKPOINT COMPACTION") || strings.TrimSpace(prefix) == "" {
		t.Fatalf("compaction assets are incomplete: %q / %q", compaction.Text, prefix)
	}
}

func TestSubagentInstructionsOnlyIncludeVisibleReadToolGuidance(t *testing.T) {
	text, err := RenderSubagentDeveloperInstructions(mustModelMessages(t), []string{"read", "grep"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## `read`") || !strings.Contains(text, "## `grep`") || strings.Contains(text, "## `edit`") {
		t.Fatalf("sub-agent tool guidance is incorrect: %q", text)
	}
}

func TestCollaborationInstructionsOnlyExposeVisibleToolGuidance(t *testing.T) {
	text, err := RenderCollaborationInstructions(mustModelMessages(t), protocol.ModeKindDefault, []string{"update_plan", "execute_command"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## `update_plan`") || !strings.Contains(text, "## `execute_command`") {
		t.Fatalf("visible Tool guidance is missing: %q", text)
	}
	if strings.Contains(text, "## `read`") || strings.Contains(text, "## `edit`") {
		t.Fatalf("hidden Tool guidance leaked: %q", text)
	}
	plan, err := RenderCollaborationInstructions(mustModelMessages(t), protocol.ModeKindPlan, []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "## Plan Mode") || !strings.Contains(plan, "## `read`") || strings.Contains(plan, "## `update_plan`") {
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
