package reflector

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/prompts"
)

type fakeClient struct {
	response llm.Response
	err      error
	request  llm.Request
}

func (client *fakeClient) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	client.request = request
	return client.response, client.err
}

func (client *fakeClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("unexpected stream call")
}

func (client *fakeClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "fake", Name: "shared-model"}
}

func (client *fakeClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}

func TestReflectorAcceptsStrictStructuredVerdict(t *testing.T) {
	client := &fakeClient{response: llm.Response{
		Message:      llm.AssistantMessage(`{"scope":"task","verdict":"accept","lesson":"keep deterministic verification"}`),
		FinishReason: llm.FinishReasonStop,
	}}
	reflector := newTestReflector(t, client)

	result, err := reflector.Reflect(context.Background(), validReflectionInput())
	if err != nil {
		t.Fatalf("reflect candidate: %v", err)
	}
	if result.Verdict != engine.ReflectionAccept || client.request.Model != "shared-model" || len(client.request.Messages) != 2 {
		t.Fatalf("unexpected reflection result/request: result=%#v request=%#v", result, client.request)
	}
	if !strings.Contains(client.request.Messages[0].Content, "chain-of-thought") {
		t.Fatalf("system prompt omitted reasoning privacy rule: %q", client.request.Messages[0].Content)
	}
	if !reflect.DeepEqual(client.request.Messages[0], llm.SystemMessage(prompts.ReflectionProtocol())) {
		t.Fatalf("reflector did not use the embedded task protocol: %q", client.request.Messages[0].Content)
	}
}

func TestReflectorReturnsRetryWithEvidenceGap(t *testing.T) {
	client := &fakeClient{response: llm.Response{
		Message:      llm.AssistantMessage(`{"scope":"task","verdict":"retry","issues":[{"code":"tests_failed","summary":"tests did not pass","severity":"critical"}],"evidence_gaps":["passing test output"],"next_action_hint":"fix the failure and rerun tests"}`),
		FinishReason: llm.FinishReasonStop,
	}}
	reflector := newTestReflector(t, client)
	input := validReflectionInput()
	input.Verification = engine.Verification{
		Status:       engine.VerificationFailed,
		Checks:       []engine.VerificationCheck{{ID: "tests", Status: engine.VerificationCheckFailed}},
		EvidenceGaps: []string{"passing test output"},
	}

	result, err := reflector.Reflect(context.Background(), input)
	if err != nil || result.Verdict != engine.ReflectionRetry || len(result.EvidenceGaps) != 1 {
		t.Fatalf("unexpected retry reflection: result=%#v err=%v", result, err)
	}
}

func TestReflectorUsesConfiguredSystemPrompt(t *testing.T) {
	client := &fakeClient{response: llm.Response{
		Message:      llm.AssistantMessage(`{"scope":"task","verdict":"accept"}`),
		FinishReason: llm.FinishReasonStop,
	}}
	reflector, err := New(client, Options{Temperature: 0.1, MaxOutputTokens: 512, SystemPrompt: " assembled reflection "})
	if err != nil {
		t.Fatalf("create configured reflector: %v", err)
	}
	if _, err := reflector.Reflect(context.Background(), validReflectionInput()); err != nil {
		t.Fatalf("run configured reflector: %v", err)
	}
	if client.request.Messages[0].Content != "assembled reflection" {
		t.Fatalf("reflector omitted configured system prompt: %#v", client.request.Messages)
	}
}

func TestReflectorRejectsFreeFormatAndUnknownReasoningFields(t *testing.T) {
	responses := []string{
		"I think the work should be accepted.",
		"```json\n{\"scope\":\"task\",\"verdict\":\"accept\"}\n```",
		`{"scope":"task","verdict":"accept","chain_of_thought":"hidden reasoning"}`,
	}
	for _, response := range responses {
		client := &fakeClient{response: llm.Response{Message: llm.AssistantMessage(response), FinishReason: llm.FinishReasonStop}}
		reflector := newTestReflector(t, client)
		if _, err := reflector.Reflect(context.Background(), validReflectionInput()); err == nil {
			t.Fatalf("expected strict JSON rejection for %q", response)
		}
	}
}

func TestReflectorRejectsAcceptWhenVerificationFailed(t *testing.T) {
	client := &fakeClient{response: llm.Response{Message: llm.AssistantMessage(`{"scope":"task","verdict":"accept"}`), FinishReason: llm.FinishReasonStop}}
	reflector := newTestReflector(t, client)
	input := validReflectionInput()
	input.Verification = engine.Verification{
		Status:       engine.VerificationFailed,
		Checks:       []engine.VerificationCheck{{ID: "tests", Status: engine.VerificationCheckFailed}},
		EvidenceGaps: []string{"tests"},
	}
	if _, err := reflector.Reflect(context.Background(), input); err == nil || !strings.Contains(err.Error(), "cannot accept") {
		t.Fatalf("unexpected failed-verification acceptance: %v", err)
	}
}

func newTestReflector(t *testing.T, client llm.Client) *Reflector {
	t.Helper()
	reflector, err := New(client, Options{Temperature: 0.1, MaxOutputTokens: 512})
	if err != nil {
		t.Fatalf("create reflector: %v", err)
	}
	return reflector
}

func validReflectionInput() engine.ReflectionInput {
	return engine.ReflectionInput{
		RunID:        "run_1",
		Task:         engine.Task{ID: "root", Objective: "complete work", Status: engine.TaskStatusReflecting},
		Candidate:    engine.CandidateTaskResult{Result: engine.TaskResult{Summary: "candidate"}, FinalMessage: llm.AssistantMessage("candidate")},
		Verification: engine.Verification{Status: engine.VerificationPassed},
	}
}
