package event

import "testing"

func TestWithEventMetadataMergesContextAndExplicitFields(t *testing.T) {
	input := TextDelta{TurnID: "explicit-run", LLMCallID: "call-1", Delta: "hello"}
	result, err := WithEventMetadata(input, Metadata{
		SessionID: "session-1",
		TurnID:    "context-run",
		TaskID:    "task-1",
		Iteration: 2,
		LLMCallID: "context-call",
	})
	if err != nil {
		t.Fatalf("merge event metadata: %v", err)
	}
	typed := result.(TextDelta)
	if typed.SessionID != "session-1" || typed.TurnID != "explicit-run" || typed.TaskID != "task-1" || typed.Iteration != 2 || typed.LLMCallID != "call-1" {
		t.Fatalf("unexpected merged metadata: %#v", MetadataFromEvent(typed))
	}
	if typed.Delta != "hello" {
		t.Fatalf("event payload changed: %#v", typed)
	}
}

func TestAllEventPayloadsExposeMetadataFields(t *testing.T) {
	events := []Event{
		LLMCallStarted{}, LLMCallCompleted{}, TextDelta{}, ReasoningDelta{}, UsageUpdated{}, ContextWindowUpdated{},
		IterationStarted{}, IterationCompleted{}, ToolCallStarted{}, ToolCallCompleted{},
		ApprovalRequested{}, ApprovalResolved{}, StatusChanged{}, DiagnosticPublished{},
		ErrorOccurred{}, TurnStarted{}, TurnStatusChanged{}, PlanUpdated{}, RunDiffUpdated{}, RunDiffInvalidated{},
		TurnCompleted{},
	}
	for _, runtimeEvent := range events {
		if _, err := WithEventMetadata(runtimeEvent, Metadata{TurnID: "run-1"}); err != nil {
			t.Fatalf("event %T does not implement metadata protocol: %v", runtimeEvent, err)
		}
	}
}
