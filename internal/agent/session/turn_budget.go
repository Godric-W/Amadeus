package session

import (
	"fmt"
	"time"
)

type TurnBudget struct {
	MaxSamples   int
	MaxToolCalls int
	MaxDuration  time.Duration
	WarnRatio    float64
}

func DefaultTurnBudget() TurnBudget {
	return TurnBudget{MaxSamples: 1_000, MaxToolCalls: 10_000, MaxDuration: 24 * time.Hour, WarnRatio: 0.9}
}

func (budget TurnBudget) Nearing(samples, toolCalls int, elapsed time.Duration) bool {
	ratio := budget.WarnRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.9
	}
	return budget.MaxSamples > 0 && float64(samples) >= float64(budget.MaxSamples)*ratio ||
		budget.MaxToolCalls > 0 && float64(toolCalls) >= float64(budget.MaxToolCalls)*ratio ||
		budget.MaxDuration > 0 && float64(elapsed) >= float64(budget.MaxDuration)*ratio
}

func (budget TurnBudget) Exhausted(samples, toolCalls int, elapsed time.Duration) string {
	switch {
	case budget.MaxSamples > 0 && samples >= budget.MaxSamples:
		return fmt.Sprintf("internal model sample safety limit reached: %d", budget.MaxSamples)
	case budget.MaxToolCalls > 0 && toolCalls >= budget.MaxToolCalls:
		return fmt.Sprintf("internal tool call safety limit reached: %d", budget.MaxToolCalls)
	case budget.MaxDuration > 0 && elapsed >= budget.MaxDuration:
		return fmt.Sprintf("internal Turn duration safety limit reached: %s", budget.MaxDuration)
	default:
		return ""
	}
}
