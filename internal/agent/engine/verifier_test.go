package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeVerifier struct {
	result Verification
	err    error
}

func (verifier *fakeVerifier) Verify(context.Context, VerificationInput) (Verification, error) {
	return verifier.result, verifier.err
}

func TestVerifierPortExpressesPassFailureAndEvidenceGaps(t *testing.T) {
	var verifier Verifier = &fakeVerifier{result: Verification{
		Status:       VerificationFailed,
		Checks:       []VerificationCheck{{ID: "tests", Status: VerificationCheckFailed}},
		EvidenceGaps: []string{"test result is missing"},
	}}
	result, err := verifier.Verify(context.Background(), validVerificationInput())
	if err != nil || result.Passed() || !reflect.DeepEqual(result.EvidenceGaps, []string{"test result is missing"}) {
		t.Fatalf("unexpected verifier contract result: result=%#v err=%v", result, err)
	}
}

func TestDeterministicVerifierPassesVerifiedCriterionEvidence(t *testing.T) {
	input := validVerificationInput()
	input.Task.AcceptanceCriteria = []Criterion{{ID: "tests", Description: "tests pass", Required: true}}
	input.Candidate.EvidenceIDs = []EvidenceID{"evidence_test"}
	input.Evidence = []Evidence{{
		ID: "evidence_test", Kind: EvidenceTest, Source: "go test", Summary: "all tests passed",
		CriterionIDs: []string{"tests"}, Verified: true,
	}}

	result, err := NewDeterministicVerifier().Verify(context.Background(), input)
	if err != nil {
		t.Fatalf("verify candidate: %v", err)
	}
	if !result.Passed() || len(result.EvidenceGaps) != 0 || len(result.Checks) != 2 {
		t.Fatalf("unexpected passed verification: %#v", result)
	}
}

func TestDeterministicVerifierReportsMissingAndUnverifiedEvidence(t *testing.T) {
	input := validVerificationInput()
	input.Task.AcceptanceCriteria = []Criterion{{ID: "tests", Description: "tests pass", Required: true}}
	input.Candidate.EvidenceIDs = []EvidenceID{"missing", "unverified"}
	input.Evidence = []Evidence{{ID: "unverified", Kind: EvidenceTest, Source: "go test", Summary: "failed", CriterionIDs: []string{"tests"}}}

	result, err := NewDeterministicVerifier().Verify(context.Background(), input)
	if err != nil {
		t.Fatalf("verify candidate: %v", err)
	}
	if result.Passed() || len(result.EvidenceGaps) != 3 {
		t.Fatalf("unexpected failed verification: %#v", result)
	}
}

func TestDeterministicVerifierSkipsUnsupportedOptionalCriterion(t *testing.T) {
	input := validVerificationInput()
	input.Task.AcceptanceCriteria = []Criterion{{ID: "docs", Description: "documentation is clear"}}

	result, err := NewDeterministicVerifier().Verify(context.Background(), input)
	if err != nil || !result.Passed() || len(result.Checks) != 1 || result.Checks[0].Status != VerificationCheckSkipped {
		t.Fatalf("unexpected optional criterion result: result=%#v err=%v", result, err)
	}
}

func TestDeterministicVerifierHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewDeterministicVerifier().Verify(ctx, validVerificationInput())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func validVerificationInput() VerificationInput {
	return VerificationInput{
		RunID:     "run_1",
		Task:      Task{ID: "root", Objective: "verify work", Status: TaskStatusVerifying},
		Candidate: TaskResult{Summary: "candidate result"},
	}
}
