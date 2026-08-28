package multiagent

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/contextmanager"
)

func TestBoundMessageUsesTokenBudgetForASCIIAndNonASCII(t *testing.T) {
	estimator := contextmanager.ApproxTokenEstimator{}
	for _, input := range []string{strings.Repeat("a", 10_000), strings.Repeat("界", 2_000)} {
		bounded := boundMessage(input)
		if estimator.EstimateText(bounded) > statusMessageTokenLimit {
			t.Fatalf("bounded message estimate = %d, limit = %d", estimator.EstimateText(bounded), statusMessageTokenLimit)
		}
		if !strings.HasSuffix(bounded, "…") {
			t.Fatalf("bounded message has no truncation marker: %q", bounded)
		}
	}
}

func TestBoundReasonReservesNotificationEnvelopeBudget(t *testing.T) {
	estimator := contextmanager.ApproxTokenEstimator{}
	bounded := boundReason(strings.Repeat("reason ", 1_000))
	if estimator.EstimateText(bounded) > statusReasonTokenLimit {
		t.Fatalf("bounded reason estimate = %d, limit = %d", estimator.EstimateText(bounded), statusReasonTokenLimit)
	}
}
