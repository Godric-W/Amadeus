package session

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func TestResolveSessionBaseUsesConfiguredValueFirst(t *testing.T) {
	configured := llm.NewCustomBaseInstructions("custom base")
	base, err := resolveSessionBase(threadstore.InitialHistory{Kind: threadstore.InitialHistoryNew}, configured, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if base != configured {
		t.Fatalf("base = %#v, want %#v", base, configured)
	}
}

func TestResolveSessionBaseUsesNeutralFallbackForUnknownModel(t *testing.T) {
	messages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	services := &SessionServices{modelInfo: llm.ModelInfo{Name: "unknown-model"}, modelMessages: messages}
	base, err := resolveSessionBase(threadstore.InitialHistory{Kind: threadstore.InitialHistoryNew}, llm.BaseInstructions{}, services, "")
	if err != nil {
		t.Fatal(err)
	}
	if base.Provenance.Type != llm.BaseInstructionsModel || base.Provenance.Model != "unknown-model" {
		t.Fatalf("fallback provenance = %#v", base.Provenance)
	}
	for _, forbidden := range []string{"based on GPT", "Go coding agent", "implemented in Go", "written in Go"} {
		if strings.Contains(base.Text, forbidden) {
			t.Fatalf("neutral fallback contains identity claim %q", forbidden)
		}
	}
}

func TestResolveSessionBaseRestoresPersistedValue(t *testing.T) {
	persisted := llm.NewModelBaseInstructions("persisted base", "old-model")
	history := threadstore.InitialHistory{Kind: threadstore.InitialHistoryResumed, Lines: []rollout.Line{{Item: rollout.SessionMetaItem{
		SessionID: testutil.SessionID(1), ID: testutil.ThreadID(1), BaseInstructions: persisted,
	}}}}
	services := &SessionServices{
		modelInfo:     llm.ModelInfo{Name: "new-model"},
		modelMessages: llm.ModelMessages{InstructionsTemplate: "new model base"},
	}
	base, err := resolveSessionBase(history, llm.BaseInstructions{}, services, "")
	if err != nil {
		t.Fatal(err)
	}
	if base != persisted {
		t.Fatalf("base = %#v, want persisted %#v", base, persisted)
	}
}

func TestResolveSessionBaseRendersCurrentModelTemplate(t *testing.T) {
	services := &SessionServices{
		modelInfo: llm.ModelInfo{Name: "current-model"},
		modelMessages: llm.ModelMessages{
			InstructionsTemplate:  "base {{ personality }}",
			InstructionsVariables: llm.ModelInstructionsVariables{PersonalityPragmatic: "pragmatic"},
		},
	}
	base, err := resolveSessionBase(threadstore.InitialHistory{Kind: threadstore.InitialHistoryNew}, llm.BaseInstructions{}, services, Personality("pragmatic"))
	if err != nil {
		t.Fatal(err)
	}
	if base.Text != "base pragmatic" || base.Provenance.Type != llm.BaseInstructionsModel || base.Provenance.Model != "current-model" {
		t.Fatalf("resolved base = %#v", base)
	}
}
