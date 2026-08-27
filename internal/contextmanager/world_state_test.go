package contextmanager

import (
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestWorldStateRendersFullThenNoChange(t *testing.T) {
	state := NewWorldState()
	section, err := NewTextSection(TextSectionOptions{ID: "environment", Kind: "environment.context", Role: llm.RoleUser, Text: "<cwd>/workspace</cwd>", OpenMarker: "<environment_context>", CloseMarker: "</environment_context>"})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Add(section); err != nil {
		t.Fatal(err)
	}
	fragments, snapshot, err := state.Render(nil, PreviousSectionAbsent)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 1 || fragments[0].Role != llm.RoleUser || fragments[0].Content != "<environment_context>\n<cwd>/workspace</cwd>\n</environment_context>" || snapshot.Revision() == "" {
		t.Fatalf("full world state = fragments %#v snapshot %#v", fragments, snapshot)
	}
	unchanged, current, err := state.Render(snapshot, PreviousSectionKnown)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged) != 0 || current.Revision() != snapshot.Revision() {
		t.Fatalf("unchanged world state = fragments %#v current %#v", unchanged, current)
	}
}

func TestWorldStateRendersReplacementAndRemoval(t *testing.T) {
	previous := WorldStateSnapshot{"agents_md": json.RawMessage(`{"text":"old"}`)}
	replacementState := NewWorldState()
	replacement, _ := NewTextSection(TextSectionOptions{ID: "agents_md", Kind: "agents_md.instructions", Role: llm.RoleUser, Text: "new", ReplacementNotice: "replace", RemovalNotice: "remove"})
	if err := replacementState.Add(replacement); err != nil {
		t.Fatal(err)
	}
	fragments, current, err := replacementState.Render(previous, PreviousSectionKnown)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 1 || fragments[0].Content != "replace\n\nnew" {
		t.Fatalf("replacement fragments = %#v", fragments)
	}

	removalState := NewWorldState()
	removal, _ := NewTextSection(TextSectionOptions{ID: "agents_md", Kind: "agents_md.instructions", Role: llm.RoleUser, RemovalNotice: "remove"})
	if err := removalState.Add(removal); err != nil {
		t.Fatal(err)
	}
	removed, empty, err := removalState.Render(current, PreviousSectionKnown)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Content != "remove" {
		t.Fatalf("removal fragments = %#v", removed)
	}
	patch := WorldStatePatch(current, empty)
	if string(patch["agents_md"]) != `{"text":""}` {
		t.Fatalf("empty section patch = %#v", patch)
	}
}

func TestApplyWorldStatePatchSupportsFullPatchAndRemoval(t *testing.T) {
	full, err := ApplyWorldStatePatch(nil, true, map[string]json.RawMessage{"a": json.RawMessage(`{"text":"one"}`), "b": json.RawMessage(`{"text":"two"}`)})
	if err != nil {
		t.Fatal(err)
	}
	patched, err := ApplyWorldStatePatch(full, false, map[string]json.RawMessage{"a": json.RawMessage(`{"text":"changed"}`), "b": json.RawMessage("null")})
	if err != nil {
		t.Fatal(err)
	}
	if string(patched["a"]) != `{"text":"changed"}` || patched["b"] != nil {
		t.Fatalf("patched snapshot = %#v", patched)
	}
}

func TestWorldStateUnknownBaselineRendersReplacement(t *testing.T) {
	state := NewWorldState()
	section, err := NewTextSection(TextSectionOptions{
		ID: "agents_md", Kind: "agents_md.instructions", Role: llm.RoleUser,
		Text: "current instructions", ReplacementNotice: "replace unknown instructions",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Add(section); err != nil {
		t.Fatal(err)
	}
	fragments, _, err := state.Render(WorldStateSnapshot{"agents_md": json.RawMessage(`{"text":"old instructions"}`)}, PreviousSectionUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 1 || fragments[0].Content != "replace unknown instructions\n\ncurrent instructions" {
		t.Fatalf("unknown baseline fragments = %#v", fragments)
	}
}

func TestWorldStateRevisionNormalizesLineEndings(t *testing.T) {
	revision := func(text string) string {
		state := NewWorldState()
		section, err := NewTextSection(TextSectionOptions{ID: "agents_md", Kind: "agents_md.instructions", Role: llm.RoleUser, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Add(section); err != nil {
			t.Fatal(err)
		}
		_, snapshot, err := state.Render(nil, PreviousSectionAbsent)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot.Revision()
	}
	if lf, crlf := revision("first\nsecond"), revision("first\r\nsecond"); lf != crlf {
		t.Fatalf("line endings changed WorldState revision: lf=%s crlf=%s", lf, crlf)
	}
}

func TestManagerTracksAbsentUnknownKnownWorldStateLifecycle(t *testing.T) {
	manager := NewManager(nil)
	if kind := manager.WorldStateBaselineKind(); kind != PreviousSectionAbsent {
		t.Fatalf("initial baseline kind = %q", kind)
	}
	contextItem, err := rollout.NewContextResponseItem(llm.UserMessage("runtime context"), rollout.ContextKindWorldState)
	if err != nil {
		t.Fatal(err)
	}
	contextItem = rollout.ScopeItem(contextItem, testutil.ThreadID(1), "turn-1").(rollout.ResponseItem)
	if err := manager.Record(1, contextItem); err != nil {
		t.Fatal(err)
	}
	if kind := manager.WorldStateBaselineKind(); kind != PreviousSectionUnknown {
		t.Fatalf("baseline after durable fragment = %q", kind)
	}
	worldState := rollout.ScopeItem(rollout.WorldStateItem{
		Full: true, Sections: map[string]json.RawMessage{"environment": json.RawMessage(`{"text":"workspace"}`)},
	}, testutil.ThreadID(1), "turn-1")
	if err := manager.Record(2, worldState); err != nil {
		t.Fatal(err)
	}
	if kind := manager.WorldStateBaselineKind(); kind != PreviousSectionKnown {
		t.Fatalf("baseline after WorldStateItem = %q", kind)
	}
	if _, known := manager.WorldStateBaseline(); !known {
		t.Fatal("known baseline was not exposed")
	}
	explicitSkill, err := rollout.NewContextResponseItem(llm.UserMessage("skill body"), rollout.ContextKindExplicitSkill)
	if err != nil {
		t.Fatal(err)
	}
	explicitSkill = rollout.ScopeItem(explicitSkill, testutil.ThreadID(1), "turn-1").(rollout.ResponseItem)
	if err := manager.Record(3, explicitSkill); err != nil {
		t.Fatal(err)
	}
	if kind := manager.WorldStateBaselineKind(); kind != PreviousSectionKnown {
		t.Fatalf("explicit Skill changed baseline kind to %q", kind)
	}
	changedContext, err := rollout.NewContextResponseItem(llm.UserMessage("changed runtime context"), rollout.ContextKindWorldState)
	if err != nil {
		t.Fatal(err)
	}
	changedContext = rollout.ScopeItem(changedContext, testutil.ThreadID(1), "turn-1").(rollout.ResponseItem)
	if err := manager.Record(4, changedContext); err != nil {
		t.Fatal(err)
	}
	if kind := manager.WorldStateBaselineKind(); kind != PreviousSectionUnknown {
		t.Fatalf("durable WorldState diff did not make baseline unknown: %q", kind)
	}
}
