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
		Concurrency: ToolConcurrencyShared,
	}
	clonedSpec := spec.Clone()
	clonedSpec.InputSchema[0] = '['
	if string(spec.InputSchema) != `{"type":"object"}` {
		t.Fatalf("spec clone shares mutable fields: %#v", spec)
	}

	call := NewCall(" call_1 ", " read_file ", json.RawMessage(`{"path":"a"}`))
	clonedCall := call.Clone()
	clonedCall.Arguments[0] = '['
	if call.ID != "call_1" || call.Name != "read_file" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("unexpected call clone behavior: %#v", call)
	}
}

func TestPreparedCallClonesCallAndTargets(t *testing.T) {
	call := NewCall("call", "read_file", json.RawMessage(`{"path":"a"}`))
	prepared, err := NewPreparedCall(call, PreparedOptions{Targets: []PreparedTarget{{Kind: TargetFilesystem, Access: TargetAccessRead, CanonicalPath: "/tmp/a"}}, Payload: "payload"})
	if err != nil {
		t.Fatal(err)
	}
	clonedCall := prepared.Call()
	clonedCall.Arguments[0] = '['
	targets := prepared.Targets()
	targets[0].CanonicalPath = "/changed"
	if string(prepared.Call().Arguments) != `{"path":"a"}` || prepared.Targets()[0].CanonicalPath != "/tmp/a" {
		t.Fatal("prepared call exposes mutable call or targets")
	}
	if payload, ok := PreparedPayloadAs[string](prepared); !ok || payload != "payload" {
		t.Fatalf("unexpected payload: %q %v", payload, ok)
	}
}

func TestSideEffectAndToolConcurrencyValidation(t *testing.T) {
	for _, effect := range []SideEffect{SideEffectNone, SideEffectRead, SideEffectWrite, SideEffectExecute, SideEffectNetwork} {
		if !effect.Valid() {
			t.Fatalf("known side effect is invalid: %q", effect)
		}
	}
	if SideEffect("mutate").Valid() {
		t.Fatal("unknown side effect is valid")
	}
	for _, concurrency := range []ToolConcurrency{ToolConcurrencyShared, ToolConcurrencyExclusive} {
		if !concurrency.Valid() {
			t.Fatalf("known tool concurrency is invalid: %q", concurrency)
		}
	}
	if ToolConcurrency("dynamic").Valid() {
		t.Fatal("unknown tool concurrency is valid")
	}
}
