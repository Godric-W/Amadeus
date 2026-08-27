package llm

import (
	"strings"
	"testing"
)

func TestModelMessagesResolveBaseInstructionsUsesPersonalityVariables(t *testing.T) {
	messages := ModelMessages{
		InstructionsTemplate: "base\n{{ personality }}",
		InstructionsVariables: ModelInstructionsVariables{
			PersonalityDefault: "default style", PersonalityFriendly: "friendly style", PersonalityPragmatic: "pragmatic style",
		},
	}
	for _, test := range []struct {
		name, personality, want string
	}{
		{name: "default", want: "default style"},
		{name: "friendly", personality: "friendly", want: "friendly style"},
		{name: "pragmatic", personality: "pragmatic", want: "pragmatic style"},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, err := messages.ResolveBaseInstructions(test.personality, "test-model")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(base.Text, test.want) || strings.Contains(base.Text, "{{ personality }}") {
				t.Fatalf("resolved base instructions = %q", base.Text)
			}
			if base.Provenance.Type != BaseInstructionsModel || base.Provenance.Model != "test-model" {
				t.Fatalf("resolved base provenance = %#v", base.Provenance)
			}
		})
	}
}

func TestModelMessagesRejectsUnknownPersonality(t *testing.T) {
	_, err := (ModelMessages{InstructionsTemplate: "base"}).ResolveBaseInstructions("unknown", "test-model")
	if err == nil {
		t.Fatal("unknown personality was accepted")
	}
}

func TestPromptDomainRevisionsChangeWhenToolSpecChanges(t *testing.T) {
	first := ToolSpec{Name: "read", Description: "read", InputSchema: []byte(`{"type":"object"}`)}
	second := first
	second.Description = "read a file"
	if first.RevisionID() == second.RevisionID() {
		t.Fatal("ToolSpec revision did not change after a semantic change")
	}
	prompt := Prompt{BaseInstructions: BaseInstructions{Text: "base"}, Tools: []ToolSpec{first}}
	changed := prompt
	changed.Tools = []ToolSpec{second}
	if prompt.RevisionID() == changed.RevisionID() {
		t.Fatal("Prompt revision did not change after a ToolSpec change")
	}
}

func TestRequestToolSpecsCloneInputAndOutputSchemas(t *testing.T) {
	request := Request{Prompt: Prompt{Tools: []ToolSpec{{
		Name: "read", InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{"type":"object"}`),
	}}}}
	cloned := request.ToolSpecs()
	cloned[0].InputSchema[0] = '['
	cloned[0].OutputSchema[0] = '['
	if request.Prompt.Tools[0].InputSchema[0] == '[' || request.Prompt.Tools[0].OutputSchema[0] == '[' {
		t.Fatal("request ToolSpec clone shares schema storage")
	}
}
