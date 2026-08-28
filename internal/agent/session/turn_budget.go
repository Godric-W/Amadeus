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
	return budget.NearingReason(samples, toolCalls, elapsed) != ""
}

func (budget TurnBudget) NearingReason(samples, toolCalls int, elapsed time.Duration) string {
	ratio := budget.WarnRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.9
	}
	switch {
	case budget.MaxSamples > 0 && float64(samples) >= float64(budget.MaxSamples)*ratio:
		return fmt.Sprintf("internal model sample safety limit approaching: %d", budget.MaxSamples)
	case budget.MaxToolCalls > 0 && float64(toolCalls) >= float64(budget.MaxToolCalls)*ratio:
		return fmt.Sprintf("internal tool call safety limit approaching: %d", budget.MaxToolCalls)
	case budget.MaxDuration > 0 && float64(elapsed) >= float64(budget.MaxDuration)*ratio:
		return fmt.Sprintf("internal Turn duration safety limit approaching: %s", budget.MaxDuration)
	default:
		return ""
	}
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

func (budget TurnBudget) RemainingDuration(elapsed time.Duration) time.Duration {
	if budget.MaxDuration <= 0 {
		return 0
	}
	remaining := budget.MaxDuration - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}
