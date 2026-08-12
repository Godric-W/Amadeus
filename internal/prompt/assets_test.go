package prompt

import (
	"strings"
	"testing"
)

func TestLoadAssetsSeparatesAgentAndCompactionInstructions(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(assets.Base.Text, "You are Amadeus") || !strings.Contains(assets.Base.Text, "Working Method") {
		t.Fatalf("BaseInstructions are incomplete: %q", assets.Base.Text)
	}
	if !strings.Contains(assets.Compaction.Text, "covered canonical history") || strings.Contains(assets.Compaction.Text, "apply_patch") || strings.Contains(assets.Compaction.Text, "execute_command") {
		t.Fatalf("Compaction instructions contain an invalid responsibility: %q", assets.Compaction.Text)
	}
}

func TestDeveloperInstructionsExposeOnlyVisibleToolGuidance(t *testing.T) {
	text, err := DeveloperInstructions("execute", []string{"update_plan", "execute_command"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Execute Mode") || !strings.Contains(text, "## `update_plan`") || !strings.Contains(text, "## `execute_command`") {
		t.Fatalf("visible Tool guidance is missing: %q", text)
	}
	if strings.Contains(text, "## `apply_patch`") || strings.Contains(text, "request_permissions") || strings.Contains(text, "{{") {
		t.Fatalf("Developer Instructions leaked hidden Tool or unresolved variable: %q", text)
	}
	plan, err := DeveloperInstructions("plan", []string{"execute_command"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "## Plan Mode") || strings.Contains(plan, "Tool Discipline") || strings.Contains(plan, "execute_command") {
		t.Fatalf("Plan Mode contains execution guidance: %q", plan)
	}
}
