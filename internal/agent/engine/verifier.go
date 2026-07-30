package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type VerificationStatus string

const (
	VerificationPassed VerificationStatus = "passed"
	VerificationFailed VerificationStatus = "failed"
)

func (status VerificationStatus) Valid() bool {
	return status == VerificationPassed || status == VerificationFailed
}

type VerificationCheckStatus string

const (
	VerificationCheckPassed  VerificationCheckStatus = "passed"
	VerificationCheckFailed  VerificationCheckStatus = "failed"
	VerificationCheckSkipped VerificationCheckStatus = "skipped"
)

func (status VerificationCheckStatus) Valid() bool {
	switch status {
	case VerificationCheckPassed, VerificationCheckFailed, VerificationCheckSkipped:
		return true
	default:
		return false
	}
}

type VerificationInput struct {
	RunID     RunID      `json:"run_id"`
	Task      Task       `json:"task"`
	Candidate TaskResult `json:"candidate"`
	Evidence  []Evidence `json:"evidence,omitempty"`
}

func (input VerificationInput) Validate() error {
	if strings.TrimSpace(string(input.RunID)) == "" {
		return errors.New("verification input run ID is empty")
	}
	if strings.TrimSpace(string(input.Task.ID)) == "" {
		return errors.New("verification input task ID is empty")
	}
	if input.Task.Status != TaskStatusVerifying {
		return fmt.Errorf("verification input task status must be %q", TaskStatusVerifying)
	}
	if strings.TrimSpace(input.Candidate.Summary) == "" {
		return errors.New("verification input candidate summary is empty")
	}
	return nil
}

type VerificationCheck struct {
	ID          string                  `json:"id"`
	Description string                  `json:"description"`
	Status      VerificationCheckStatus `json:"status"`
	EvidenceIDs []EvidenceID            `json:"evidence_ids,omitempty"`
	Detail      string                  `json:"detail,omitempty"`
}

type Verification struct {
	Status       VerificationStatus  `json:"status"`
	Checks       []VerificationCheck `json:"checks"`
	EvidenceGaps []string            `json:"evidence_gaps,omitempty"`
}

func (verification Verification) Passed() bool {
	return verification.Status == VerificationPassed
}

func (verification Verification) Validate() error {
	if !verification.Status.Valid() {
		return fmt.Errorf("verification status %q is invalid", verification.Status)
	}
	for index, check := range verification.Checks {
		if strings.TrimSpace(check.ID) == "" {
			return fmt.Errorf("verification check %d ID is empty", index)
		}
		if !check.Status.Valid() {
			return fmt.Errorf("verification check %q status %q is invalid", check.ID, check.Status)
		}
	}
	if verification.Status == VerificationPassed {
		if len(verification.EvidenceGaps) != 0 {
			return errors.New("passed verification cannot contain evidence gaps")
		}
		for _, check := range verification.Checks {
			if check.Status == VerificationCheckFailed {
				return errors.New("passed verification cannot contain failed checks")
			}
		}
	}
	if verification.Status == VerificationFailed && len(verification.EvidenceGaps) == 0 {
		return errors.New("failed verification requires evidence gaps")
	}
	return nil
}

type Verifier interface {
	Verify(context.Context, VerificationInput) (Verification, error)
}

type DeterministicVerifier struct{}

func NewDeterministicVerifier() *DeterministicVerifier {
	return &DeterministicVerifier{}
}

func (verifier *DeterministicVerifier) Verify(ctx context.Context, input VerificationInput) (Verification, error) {
	if err := input.Validate(); err != nil {
		return Verification{}, err
	}
	if err := ctx.Err(); err != nil {
		return Verification{}, err
	}

	evidenceByID := make(map[EvidenceID]Evidence, len(input.Evidence))
	for _, item := range input.Evidence {
		if strings.TrimSpace(string(item.ID)) == "" {
			continue
		}
		evidenceByID[item.ID] = item
	}

	checks := make([]VerificationCheck, 0, len(input.Candidate.EvidenceIDs)+len(input.Task.AcceptanceCriteria))
	gaps := make([]string, 0)
	candidateEvidence := make(map[EvidenceID]Evidence, len(input.Candidate.EvidenceIDs))
	seen := make(map[EvidenceID]struct{}, len(input.Candidate.EvidenceIDs))
	for _, evidenceID := range input.Candidate.EvidenceIDs {
		if _, duplicate := seen[evidenceID]; duplicate {
			continue
		}
		seen[evidenceID] = struct{}{}
		item, ok := evidenceByID[evidenceID]
		check := VerificationCheck{ID: "evidence:" + string(evidenceID), Description: "candidate evidence is present and verified", EvidenceIDs: []EvidenceID{evidenceID}}
		switch {
		case !ok:
			check.Status = VerificationCheckFailed
			check.Detail = "candidate references missing evidence"
			gaps = append(gaps, fmt.Sprintf("evidence %q is missing", evidenceID))
		case !item.Verified:
			check.Status = VerificationCheckFailed
			check.Detail = "candidate evidence is not verified"
			gaps = append(gaps, fmt.Sprintf("evidence %q is not verified", evidenceID))
		default:
			check.Status = VerificationCheckPassed
			candidateEvidence[evidenceID] = item
		}
		checks = append(checks, check)
	}

	for _, criterion := range input.Task.AcceptanceCriteria {
		matching := criterionEvidence(candidateEvidence, criterion.ID)
		check := VerificationCheck{
			ID:          "criterion:" + criterion.ID,
			Description: criterion.Description,
			EvidenceIDs: matching,
		}
		switch {
		case len(matching) != 0:
			check.Status = VerificationCheckPassed
		case criterion.Required:
			check.Status = VerificationCheckFailed
			check.Detail = "required acceptance criterion has no verified candidate evidence"
			gaps = append(gaps, fmt.Sprintf("required criterion %q lacks verified evidence", criterion.ID))
		default:
			check.Status = VerificationCheckSkipped
			check.Detail = "optional acceptance criterion has no verified candidate evidence"
		}
		checks = append(checks, check)
	}

	status := VerificationPassed
	if len(gaps) != 0 {
		status = VerificationFailed
	}
	verification := Verification{Status: status, Checks: checks, EvidenceGaps: gaps}
	return verification, verification.Validate()
}

func criterionEvidence(evidence map[EvidenceID]Evidence, criterionID string) []EvidenceID {
	matched := make([]EvidenceID, 0)
	for evidenceID, item := range evidence {
		for _, candidate := range item.CriterionIDs {
			if candidate == criterionID {
				matched = append(matched, evidenceID)
				break
			}
		}
	}
	sortEvidenceIDs(matched)
	return matched
}

func sortEvidenceIDs(ids []EvidenceID) {
	for index := 1; index < len(ids); index++ {
		for current := index; current > 0 && ids[current] < ids[current-1]; current-- {
			ids[current], ids[current-1] = ids[current-1], ids[current]
		}
	}
}

var _ Verifier = (*DeterministicVerifier)(nil)
