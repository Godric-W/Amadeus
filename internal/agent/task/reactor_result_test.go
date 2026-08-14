package task

import (
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestConfiguredAgentBudgetMapsStructuralRunLimits(t *testing.T) {
	budget := configuredReactorBudget(config.AgentConfig{
		MaxIterations: 7, MaxToolCalls: 11, MaxDuration: 19 * time.Minute, MaxParallelTools: 3,
	})
	if budget.Budget.MaxIterations != 7 || budget.Budget.MaxToolCalls != 11 || budget.Budget.MaxDuration != 19*time.Minute {
		t.Fatalf("unexpected configured Agent budget: %#v", budget)
	}
}
