package session

import (
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestDomainConstructorsNormalizeInitialState(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	project, err := NewProject("project-1", filepath.Clean(t.TempDir()), "Example", now)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := NewConversationSession("session-1", project.ID, "Fix tests", now)
	if err != nil {
		t.Fatalf("create conversation session: %v", err)
	}
	turn, err := NewTurn("turn-1", conversation.ID, 1, now)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	run, err := NewRun("run-1", conversation.ID, turn.ID, "Fix tests", now)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if project.CreatedAt.Location() != time.UTC || conversation.Status != ConversationSessionActive || conversation.NextTurnSequence != 1 || turn.Status != TurnRunning || turn.CompletedAt != nil || run.Status != RunRunning || run.LatestCheckpointSequence != 0 || run.FinishedAt != nil {
		t.Fatalf("unexpected initial domain state: project=%#v session=%#v turn=%#v run=%#v", project, conversation, turn, run)
	}
}

func TestConversationSessionAllocatesSequencesAndCanArchiveOrReactivate(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	conversation, err := NewConversationSession("session-1", "project-1", "Title", now)
	if err != nil {
		t.Fatalf("create conversation session: %v", err)
	}
	first, err := conversation.AllocateTurnSequence(now.Add(time.Second))
	if err != nil || first != 1 || conversation.NextTurnSequence != 2 {
		t.Fatalf("allocate first sequence: sequence=%d session=%#v err=%v", first, conversation, err)
	}
	if err := conversation.Transition(ConversationSessionArchived, now.Add(2*time.Second)); err != nil {
		t.Fatalf("archive conversation session: %v", err)
	}
	if err := conversation.Transition(ConversationSessionActive, now.Add(3*time.Second)); err != nil {
		t.Fatalf("reactivate conversation session: %v", err)
	}
	if err := conversation.Transition(ConversationSessionActive, now.Add(4*time.Second)); !isTransitionError(err) {
		t.Fatalf("same-state session transition was accepted: %v", err)
	}
	conversation.NextTurnSequence = math.MaxInt64
	if _, err := conversation.AllocateTurnSequence(now.Add(5 * time.Second)); err == nil {
		t.Fatal("exhausted turn sequence was allocated")
	}
}

func TestTurnTransitionsFromRunningToEveryTerminalOutcome(t *testing.T) {
	statuses := []TurnStatus{TurnCompleted, TurnCancelled, TurnFailed, TurnPartial, TurnNeedsPlan, TurnAwaitingUser}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			createdAt := time.Unix(100, 0).UTC()
			turn, err := NewTurn("turn-1", "session-1", 1, createdAt)
			if err != nil {
				t.Fatalf("create turn: %v", err)
			}
			if err := turn.Complete(status, createdAt.Add(time.Second)); err != nil {
				t.Fatalf("complete turn as %s: %v", status, err)
			}
			if turn.Status != status || turn.CompletedAt == nil || !turn.CompletedAt.Equal(createdAt.Add(time.Second)) {
				t.Fatalf("unexpected terminal turn: %#v", turn)
			}
			if err := turn.Complete(TurnCompleted, createdAt.Add(2*time.Second)); !isTransitionError(err) {
				t.Fatalf("terminal turn transitioned again: %v", err)
			}
		})
	}
}

func TestRunTransitionsAndPreservesPersistentMetadata(t *testing.T) {
	statuses := []RunStatus{RunCompleted, RunCancelled, RunFailed, RunPartial, RunNeedsPlan, RunAwaitingUser}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			startedAt := time.Unix(100, 0).UTC()
			run, err := NewRun("run-1", "session-1", "turn-1", "Fix tests", startedAt)
			if err != nil {
				t.Fatalf("create run: %v", err)
			}
			run.ContextFromRunID = "run-previous"
			run.Provider = "openai"
			run.Model = "model"
			run.APIMode = "responses"
			run.Dialect = "openai"
			run.BudgetJSON = json.RawMessage(`{"max_steps":10}`)
			run.UsageJSON = json.RawMessage(`{"input_tokens":20}`)
			reason := ""
			if status != RunCompleted {
				reason = "terminal outcome"
			}
			if err := run.Finish(status, reason, startedAt.Add(time.Second)); err != nil {
				t.Fatalf("finish run as %s: %v", status, err)
			}
			if run.Status != status || run.FinishedAt == nil || run.ContextFromRunID != "run-previous" {
				t.Fatalf("unexpected terminal run: %#v", run)
			}
			if err := run.Finish(RunCompleted, "", startedAt.Add(2*time.Second)); !isTransitionError(err) {
				t.Fatalf("terminal run transitioned again: %v", err)
			}
		})
	}
}

func TestDomainValidationRejectsInvalidIDsSequencesTimesAndPayloads(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	if _, err := NewProject("bad id", t.TempDir(), "Project", now); err == nil {
		t.Fatal("project accepted whitespace ID")
	}
	if _, err := NewProject("project-1", "relative", "Project", now); err == nil {
		t.Fatal("project accepted relative canonical path")
	}
	if _, err := NewConversationSession("session-1", "", "Title", now); err == nil {
		t.Fatal("conversation session accepted empty project ID")
	}
	if _, err := NewTurn("turn-1", "session-1", 0, now); err == nil {
		t.Fatal("turn accepted zero sequence")
	}
	turn, _ := NewTurn("turn-1", "session-1", 1, now)
	if err := turn.Complete(TurnRunning, now.Add(time.Second)); !isTransitionError(err) {
		t.Fatalf("turn accepted non-terminal completion: %v", err)
	}
	if err := turn.Complete(TurnCompleted, now.Add(-time.Second)); err == nil {
		t.Fatal("turn accepted completion before creation")
	}
	run, _ := NewRun("run-1", "session-1", "turn-1", "Objective", now)
	run.ContextFromRunID = run.ID
	if err := run.Validate(); err == nil {
		t.Fatal("run accepted itself as interrupted context")
	}
	run.ContextFromRunID = ""
	run.BudgetJSON = json.RawMessage(`{"broken"`)
	if err := run.Validate(); err == nil {
		t.Fatal("run accepted invalid budget JSON")
	}
	run.BudgetJSON = nil
	if err := run.Finish(RunFailed, "", now.Add(time.Second)); err == nil {
		t.Fatal("failed run accepted empty stop reason")
	}
}

func isTransitionError(err error) bool {
	var transition *TransitionError
	return errors.As(err, &transition)
}
