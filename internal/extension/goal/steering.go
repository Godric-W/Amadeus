package goal

import (
	_ "embed"
	"strconv"
	"strings"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

//go:embed templates/continuation.md
var continuationTemplate string

//go:embed templates/budget_limit.md
var budgetLimitTemplate string

//go:embed templates/objective_updated.md
var objectiveUpdatedTemplate string

func continuationInput(goal protocol.ThreadGoal) (agentsession.ResponseItemTurnInput, error) {
	return renderGoalInput(continuationTemplate, goal)
}

func budgetLimitInput(goal protocol.ThreadGoal) (agentsession.ResponseItemTurnInput, error) {
	return renderGoalInput(budgetLimitTemplate, goal)
}

func objectiveUpdatedInput(goal protocol.ThreadGoal) (agentsession.ResponseItemTurnInput, error) {
	return renderGoalInput(objectiveUpdatedTemplate, goal)
}

func renderGoalInput(template string, goal protocol.ThreadGoal) (agentsession.ResponseItemTurnInput, error) {
	budget := "none"
	remaining := "unbounded"
	if goal.TokenBudget != nil {
		budget = strconv.FormatInt(*goal.TokenBudget, 10)
		remaining = strconv.FormatInt(max(*goal.TokenBudget-goal.TokensUsed, 0), 10)
	}
	content := strings.NewReplacer(
		"{{ objective }}", escapeXML(goal.Objective),
		"{{ tokens_used }}", strconv.FormatInt(goal.TokensUsed, 10),
		"{{ token_budget }}", budget,
		"{{ remaining_tokens }}", remaining,
		"{{ time_used_seconds }}", strconv.FormatInt(goal.TimeUsedSeconds, 10),
	).Replace(template)
	item, err := rollout.NewContextResponseItem(llm.UserMessage(content), rollout.ContextKindGoal)
	if err != nil {
		return agentsession.ResponseItemTurnInput{}, err
	}
	return agentsession.ResponseItemTurnInput{Item: item}, nil
}

func escapeXML(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}
