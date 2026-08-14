package tool

import (
	"encoding/json"
	"testing"
)

func TestToolCallAndSpecCloneIsolateMutableFields(t *testing.T) {
	spec := ToolSpec{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`), SideEffect: SideEffectRead}
	clonedSpec := spec.Clone()
	clonedSpec.InputSchema[0] = '['
	if spec.InputSchema[0] == '[' {
		t.Fatal("spec clone shares input schema")
	}
	call := NewCall("call-1", "read", json.RawMessage(`{"path":"a"}`))
	clonedCall := call.Clone()
	clonedCall.Payload[0] = '['
	if call.Payload[0] == '[' {
		t.Fatal("call clone shares arguments")
	}
}

func TestToolOutputAndExecutionCloneMetadata(t *testing.T) {
	output := ToolResult{CallID: "call-1", ToolName: "read", Metadata: map[string]any{"path": "a"}}
	cloned := output.Clone()
	cloned.Metadata["path"] = "b"
	if output.Metadata["path"] != "a" {
		t.Fatal("output clone shares metadata")
	}
	execution := ToolExecution{Call: NewCall("call-1", "read", nil), Output: output, Outcome: ToolCallOutcome{Status: ToolCallCompleted}}
	if execution.Outcome.Status != ToolCallCompleted || !execution.Outcome.Status.Valid() {
		t.Fatalf("unexpected execution: %#v", execution)
	}
}

func TestSideEffectValidation(t *testing.T) {
	for _, effect := range []SideEffect{SideEffectNone, SideEffectRead, SideEffectWrite, SideEffectExecute, SideEffectNetwork} {
		if !effect.Valid() {
			t.Fatalf("expected valid side effect %q", effect)
		}
	}
	if SideEffect("dynamic").Valid() {
		t.Fatal("unexpected dynamic side effect")
	}
}
