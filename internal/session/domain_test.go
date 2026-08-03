package session

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestConversationSessionAllocatesRunSequences(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	conversation, err := NewConversationSession("session-1", "project-1", "Title", now)
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := conversation.AllocateRunSequence(now.Add(time.Second))
	if err != nil || sequence != 1 || conversation.NextRunSequence != 2 {
		t.Fatalf("unexpected run sequence allocation: session=%#v sequence=%d err=%v", conversation, sequence, err)
	}
}

func TestRunTransitionsAndValidation(t *testing.T) {
	startedAt := time.Unix(100, 0).UTC()
	for _, status := range []RunStatus{RunCompleted, RunInterrupted, RunFailed} {
		t.Run(string(status), func(t *testing.T) {
			run, err := NewRun("run-1", "session-1", "Objective", startedAt)
			if err != nil {
				t.Fatal(err)
			}
			run.ContextFromRunID = "run-previous"
			run.UsageJSON = json.RawMessage(`{"input_tokens":10}`)
			reason := ""
			if status != RunCompleted {
				reason = "terminal outcome"
			}
			if err := run.Finish(status, reason, startedAt.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := run.Finish(RunCompleted, "", startedAt.Add(2*time.Second)); !isTransitionError(err) {
				t.Fatalf("terminal run transitioned again: %v", err)
			}
		})
	}
}

func TestDomainValidationRejectsInvalidRunAndMessageFields(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	if _, err := NewProject("bad id", t.TempDir(), "Project", now); err == nil {
		t.Fatal("project accepted whitespace ID")
	}
	if _, err := NewConversationSession("session-1", "", "Title", now); err == nil {
		t.Fatal("conversation accepted empty project ID")
	}
	run, _ := NewRun("run-1", "session-1", "Objective", now)
	run.ContextFromRunID = run.ID
	if err := run.Validate(); err == nil {
		t.Fatal("run accepted itself as interrupted context")
	}
	run.ContextFromRunID = ""
	run.UsageJSON = json.RawMessage(`{"broken"`)
	if err := run.Validate(); err == nil {
		t.Fatal("run accepted invalid usage JSON")
	}
	if _, err := NewMessage("message-1", "session-1", "", 1, MessageUser, "content", now); err == nil {
		t.Fatal("message accepted missing run ID")
	}
	if err := run.Finish(RunFailed, "", now.Add(time.Second)); err == nil {
		t.Fatal("failed run accepted empty stop reason")
	}
}

func isTransitionError(err error) bool {
	var transition *TransitionError
	return errors.As(err, &transition)
}
