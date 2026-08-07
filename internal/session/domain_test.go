package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionRunAndRolloutValidation(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	value, err := NewSession("session-1", "project-1", "Title", now)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if value.NextRunSequence != 1 || value.NextItemSequence != 1 || value.Status != SessionActive {
		t.Fatalf("unexpected session: %#v", value)
	}
	run, err := NewRun("run-1", value.ID, 1, RunModePlan, now)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	if err := run.Finish(RunCompleted, "", json.RawMessage(`{"input_tokens":4}`), now.Add(time.Second)); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	payload, _ := EncodePayload(UserMessagePayload{Content: "hello"})
	item, err := NewRolloutItem("item-1", value.ID, run.ID, 1, RolloutUserMessage, payload, now)
	if err != nil {
		t.Fatalf("new rollout item: %v", err)
	}
	decoded, err := DecodeUserMessage(item)
	if err != nil || decoded.Content != "hello" {
		t.Fatalf("decode user item: %#v err=%v", decoded, err)
	}
}

func TestDomainRejectsInvalidCanonicalState(t *testing.T) {
	now := time.Now().UTC()
	if _, err := NewSession("session-1", "", "Title", now); err == nil {
		t.Fatal("session without project was accepted")
	}
	if _, err := NewRun("run-1", "session-1", 1, "planned", now); err == nil {
		t.Fatal("legacy run mode was accepted")
	}
	if _, err := NewRolloutItem("item-1", "session-1", "run-1", 1, RolloutUserMessage, json.RawMessage(`{"broken"`), now); err == nil {
		t.Fatal("invalid rollout payload was accepted")
	}
}
