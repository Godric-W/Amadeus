package tool

import (
	"encoding/json"
	"testing"
)

func TestCallAndSpecCloneIsolateMutableFields(t *testing.T) {
	spec := Spec{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		SideEffect:  SideEffectRead,
		ResourceStrategy: ResourceStrategy{
			Mode:          ResourceModeArguments,
			ArgumentPaths: []string{"path"},
		},
	}
	clonedSpec := spec.Clone()
	clonedSpec.InputSchema[0] = '['
	clonedSpec.ResourceStrategy.ArgumentPaths[0] = "other"
	if string(spec.InputSchema) != `{"type":"object"}` || spec.ResourceStrategy.ArgumentPaths[0] != "path" {
		t.Fatalf("spec clone shares mutable fields: %#v", spec)
	}

	call := NewCall(" call_1 ", " read_file ", json.RawMessage(`{"path":"a"}`))
	clonedCall := call.Clone()
	clonedCall.Arguments[0] = '['
	if call.ID != "call_1" || call.Name != "read_file" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("unexpected call clone behavior: %#v", call)
	}
}

func TestSideEffectAndResourceModeValidation(t *testing.T) {
	for _, effect := range []SideEffect{SideEffectNone, SideEffectRead, SideEffectWrite, SideEffectExecute, SideEffectNetwork} {
		if !effect.Valid() {
			t.Fatalf("known side effect is invalid: %q", effect)
		}
	}
	if SideEffect("mutate").Valid() {
		t.Fatal("unknown side effect is valid")
	}
	for _, mode := range []ResourceMode{ResourceModeNone, ResourceModeExclusive, ResourceModeArguments} {
		if !mode.Valid() {
			t.Fatalf("known resource mode is invalid: %q", mode)
		}
	}
	if ResourceMode("dynamic").Valid() {
		t.Fatal("unknown resource mode is valid")
	}
}
