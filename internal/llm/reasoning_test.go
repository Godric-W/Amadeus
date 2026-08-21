package llm

import "testing"

func TestReasoningEffortValidation(t *testing.T) {
	for _, effort := range []ReasoningEffort{
		ReasoningEffortNone,
		ReasoningEffortMinimal,
		ReasoningEffortLow,
		ReasoningEffortMedium,
		ReasoningEffortHigh,
		ReasoningEffortXHigh,
		ReasoningEffortMax,
	} {
		if !effort.Valid() {
			t.Errorf("effort %q should be valid", effort)
		}
	}
	for _, effort := range []ReasoningEffort{"", "ultra", "maximum"} {
		if effort.Valid() {
			t.Errorf("effort %q should be invalid", effort)
		}
	}
}

func TestReasoningConfigCloneIsIndependent(t *testing.T) {
	effort := ReasoningEffortHigh
	original := &ReasoningConfig{Effort: &effort}
	cloned := original.Clone()
	*cloned.Effort = ReasoningEffortLow
	if *original.Effort != ReasoningEffortHigh {
		t.Fatalf("clone changed original effort: %q", *original.Effort)
	}
}
