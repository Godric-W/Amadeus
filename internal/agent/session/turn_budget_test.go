package session

import (
	"strings"
	"testing"
	"time"
)

func TestTurnBudgetReportsSoftAndHardLimitsForEveryDimension(t *testing.T) {
	budget := TurnBudget{MaxSamples: 10, MaxToolCalls: 20, MaxDuration: 100 * time.Second, WarnRatio: 0.9}
	tests := []struct {
		name      string
		samples   int
		toolCalls int
		elapsed   time.Duration
		want      string
	}{
		{name: "samples soft", samples: 9, want: "sample safety limit approaching"},
		{name: "tools soft", toolCalls: 18, want: "tool call safety limit approaching"},
		{name: "duration soft", elapsed: 90 * time.Second, want: "duration safety limit approaching"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if reason := budget.NearingReason(test.samples, test.toolCalls, test.elapsed); !strings.Contains(reason, test.want) {
				t.Fatalf("soft reason = %q, want %q", reason, test.want)
			}
		})
	}
	if reason := budget.Exhausted(10, 0, 0); !strings.Contains(reason, "sample safety limit reached") {
		t.Fatalf("sample hard reason = %q", reason)
	}
	if reason := budget.Exhausted(0, 20, 0); !strings.Contains(reason, "tool call safety limit reached") {
		t.Fatalf("tool hard reason = %q", reason)
	}
	if reason := budget.Exhausted(0, 0, 100*time.Second); !strings.Contains(reason, "duration safety limit reached") {
		t.Fatalf("duration hard reason = %q", reason)
	}
	if remaining := budget.RemainingDuration(90 * time.Second); remaining != 10*time.Second {
		t.Fatalf("finalization duration reserve = %s", remaining)
	}
}
