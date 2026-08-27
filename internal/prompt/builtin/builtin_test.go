package builtin

import (
	"reflect"
	"strings"
	"testing"
)

func TestCatalogIsCompleteReadableAndImmutable(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("validate built-in Prompts: %v", err)
	}
	ids := All()
	if len(ids) == 0 {
		t.Fatal("built-in Prompt catalog is empty")
	}
	seen := make(map[ID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate built-in Prompt ID %q", id)
		}
		seen[id] = struct{}{}
		content, err := Read(id)
		if err != nil {
			t.Fatalf("read built-in Prompt %q: %v", id, err)
		}
		if strings.TrimSpace(content) == "" {
			t.Fatalf("built-in Prompt %q is empty", id)
		}
	}
	ids[0] = ContextCompaction
	if All()[0] != AgentBase {
		t.Fatal("built-in Prompt catalog shares mutable storage")
	}
	layers := AgentSystemLayers()
	layers[0] = ContextCompaction
	if AgentSystemLayers()[0] != AgentBase {
		t.Fatal("Agent System layer catalog shares mutable storage")
	}
}

func TestAgentSystemLayerOrderIsStable(t *testing.T) {
	want := []ID{AgentBase}
	if got := AgentSystemLayers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Agent System layer order: got %v, want %v", got, want)
	}
}

func TestBuiltinsDoNotReintroduceRemovedAgentArchitectures(t *testing.T) {
	for _, id := range All() {
		content, err := Read(id)
		if err != nil {
			t.Fatalf("read built-in Prompt %q: %v", id, err)
		}
		for _, forbidden := range []string{"Replanner", "PlanReviewer", "Final Synthesizer", "Verification verdict", "Reflection verdict"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("built-in Prompt %q contains removed architecture term %q", id, forbidden)
			}
		}
	}
}
