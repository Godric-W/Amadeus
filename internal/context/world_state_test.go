package agentcontext

import "testing"

func TestWorldStateFragmentsHaveStableMarkersAndRevisions(t *testing.T) {
	state := NewWorldState()
	if err := state.Set(UpdateEnvironment, "Current working directory: /workspace"); err != nil {
		t.Fatal(err)
	}
	fragment := state.Fragment(UpdateEnvironment)
	if fragment.Render() != "<environment_context>\nCurrent working directory: /workspace\n</environment_context>" {
		t.Fatalf("unexpected fragment rendering: %q", fragment.Render())
	}
	if fragment.Revision() == "" || state.Revision() == "" {
		t.Fatal("world state revisions must be populated")
	}
	first := state.Revision()
	if err := state.Set(UpdateEnvironment, "Current working directory: /workspace"); err != nil {
		t.Fatal(err)
	}
	if got := state.Revision(); got != first {
		t.Fatalf("same world state changed revision: %q != %q", got, first)
	}
}

func TestWorldStateRejectsUnknownSectionsAndDeletesEmptySections(t *testing.T) {
	state := NewWorldState()
	if err := state.Set(UpdateKey("unknown"), "value"); err == nil {
		t.Fatal("unknown section was accepted")
	}
	if err := state.Set(UpdateEnvironment, "value"); err != nil {
		t.Fatal(err)
	}
	if err := state.Set(UpdateEnvironment, ""); err != nil {
		t.Fatal(err)
	}
	if got := state.Fragment(UpdateEnvironment).Render(); got != "" {
		t.Fatalf("empty section remained: %q", got)
	}
}
