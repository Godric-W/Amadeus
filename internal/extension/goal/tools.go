package goal

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/tool"
)

const (
	getGoalToolName    = "get_goal"
	createGoalToolName = "create_goal"
	updateGoalToolName = "update_goal"
)

type goalToolKind string

const (
	goalToolGet    goalToolKind = "get"
	goalToolCreate goalToolKind = "create"
	goalToolUpdate goalToolKind = "update"
)

type goalTool struct {
	kind    goalToolKind
	runtime *RuntimeHandle
	maximum *int64
}

type createGoalArgs struct {
	Objective   string `json:"objective"`
	TokenBudget *int64 `json:"token_budget,omitempty"`
}

type updateGoalArgs struct {
	Status protocol.ThreadGoalStatus `json:"status"`
}

type goalToolResponse struct {
	Goal                   *protocol.ThreadGoal `json:"goal"`
	RemainingTokens        *int64               `json:"remainingTokens,omitempty"`
	CompletionBudgetReport string               `json:"completionBudgetReport,omitempty"`
}

func newGoalTool(kind goalToolKind, runtime *RuntimeHandle, maximum *int64) *goalTool {
	return &goalTool{kind: kind, runtime: runtime, maximum: maximum}
}

func (candidate *goalTool) Spec() tool.ToolSpec {
	switch candidate.kind {
	case goalToolGet:
		return tool.ToolSpec{Name: getGoalToolName, Description: "Get the current goal for this thread, including status, budgets, token and elapsed-time usage, and remaining token budget.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), SideEffect: tool.SideEffectNone, Idempotent: true}
	case goalToolCreate:
		return tool.ToolSpec{Name: createGoalToolName, Description: "Create a goal only when explicitly requested by the user or system/developer instructions; do not infer goals from ordinary tasks. Set token_budget only when an explicit token budget is requested. Fails if an unfinished goal exists; use update_goal only for status.", InputSchema: json.RawMessage(`{"type":"object","properties":{"objective":{"type":"string","description":"Required. The concrete objective to start pursuing. This starts a new active goal when no goal exists or replaces the current goal when it is complete."},"token_budget":{"type":"integer","description":"Positive token budget for the new goal. Omit unless explicitly requested."}},"required":["objective"],"additionalProperties":false}`), SideEffect: tool.SideEffectNone}
	default:
		return tool.ToolSpec{Name: updateGoalToolName, Description: "Update the existing goal. Use this tool only to mark the goal achieved or genuinely blocked. Set complete only when the objective is achieved and no required work remains. Set blocked only after the same blocker repeats for at least three consecutive goal turns and meaningful progress requires user input or an external-state change. Pause, resume, budget-limit, and usage-limit changes are controlled by the user or system.", InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["complete","blocked"],"description":"Required. Mark the existing goal complete or genuinely blocked."}},"required":["status"],"additionalProperties":false}`), SideEffect: tool.SideEffectNone}
	}
}

func (*goalTool) SupportsParallelToolCalls() bool { return false }

func (candidate *goalTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	switch candidate.kind {
	case goalToolGet:
		var value map[string]any
		return json.Unmarshal(invocation.Call.Payload, &value)
	case goalToolCreate:
		var args createGoalArgs
		if err := json.Unmarshal(invocation.Call.Payload, &args); err != nil {
			return err
		}
		args.Objective = strings.TrimSpace(args.Objective)
		if err := protocol.ValidateThreadGoalObjective(args.Objective); err != nil {
			return err
		}
		return validateBudget(args.TokenBudget, candidate.maximum)
	case goalToolUpdate:
		var args updateGoalArgs
		if err := json.Unmarshal(invocation.Call.Payload, &args); err != nil {
			return err
		}
		if args.Status != protocol.ThreadGoalComplete && args.Status != protocol.ThreadGoalBlocked {
			return errors.New("update_goal can only mark the existing goal complete or blocked")
		}
		return nil
	default:
		return errors.New("goal tool kind is invalid")
	}
}

func (candidate *goalTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var stateValue any
	switch candidate.kind {
	case goalToolGet:
		stateValue = struct{}{}
	case goalToolCreate:
		var args createGoalArgs
		if err := json.Unmarshal(invocation.Call.Payload, &args); err != nil {
			return tool.PreparedToolUse{}, err
		}
		args.Objective = strings.TrimSpace(args.Objective)
		stateValue = args
	case goalToolUpdate:
		var args updateGoalArgs
		if err := json.Unmarshal(invocation.Call.Payload, &args); err != nil {
			return tool.PreparedToolUse{}, err
		}
		stateValue = args
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: stateValue, State: stateValue, Permission: tool.AllowPermission()}, nil
}

func (candidate *goalTool) Execute(ctx tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	if candidate.runtime == nil {
		return tool.ToolResult{}, errors.New("goal runtime is unavailable")
	}
	switch candidate.kind {
	case goalToolGet:
		goal, err := candidate.runtime.store.Get(ctx.Context, candidate.runtime.threadID)
		if err != nil {
			return tool.ToolResult{}, err
		}
		return encodeGoalResult(getGoalToolName, goal, false)
	case goalToolCreate:
		args, ok := prepared.State.(createGoalArgs)
		if !ok {
			return tool.ToolResult{}, errors.New("create_goal preparation state is invalid")
		}
		budget := cloneInt64(args.TokenBudget)
		if budget == nil {
			budget = cloneInt64(candidate.maximum)
		}
		created, err := candidate.runtime.store.InsertIfAbsentOrComplete(ctx.Context, candidate.runtime.threadID, args.Objective, protocol.ThreadGoalActive, budget)
		if err != nil {
			return tool.ToolResult{}, err
		}
		if created == nil {
			return tool.ToolResult{}, errors.New("cannot create a new goal because this thread has an unfinished goal; complete the existing goal first")
		}
		turnID := candidate.runtime.accounting.markCurrentGoalActive(created.GoalID)
		if err := candidate.runtime.publishGoal(ctx.Context, prepared.Invocation.Call.ID, turnID, *created); err != nil {
			return tool.ToolResult{}, err
		}
		return encodeGoalResult(createGoalToolName, created, false)
	case goalToolUpdate:
		args, ok := prepared.State.(updateGoalArgs)
		if !ok {
			return tool.ToolResult{}, errors.New("update_goal preparation state is invalid")
		}
		mode := state.GoalAccountingActiveOrStopped
		if args.Status == protocol.ThreadGoalComplete {
			mode = state.GoalAccountingActiveOrComplete
		}
		turnID := candidate.runtime.accounting.currentTurnID()
		if _, err := candidate.runtime.accountTurnProgress(ctx.Context, turnID, mode, false, prepared.Invocation.Call.ID+":progress"); err != nil {
			return tool.ToolResult{}, err
		}
		current, err := candidate.runtime.store.Get(ctx.Context, candidate.runtime.threadID)
		if err != nil {
			return tool.ToolResult{}, err
		}
		if current == nil {
			return tool.ToolResult{}, errors.New("cannot update goal because this thread has no goal")
		}
		updated, err := candidate.runtime.store.Update(ctx.Context, candidate.runtime.threadID, state.GoalUpdate{Status: &args.Status, ExpectedGoalID: current.GoalID})
		if err != nil || updated == nil {
			return tool.ToolResult{}, errors.Join(err, errors.New("goal changed concurrently"))
		}
		candidate.runtime.accounting.clearCurrentGoal()
		if err := candidate.runtime.publishGoal(ctx.Context, prepared.Invocation.Call.ID, turnID, *updated); err != nil {
			return tool.ToolResult{}, err
		}
		return encodeGoalResult(updateGoalToolName, updated, args.Status == protocol.ThreadGoalComplete)
	default:
		return tool.ToolResult{}, errors.New("goal tool kind is invalid")
	}
}

func encodeGoalResult(name string, stored *state.ThreadGoal, completion bool) (tool.ToolResult, error) {
	response := goalToolResponse{}
	if stored != nil {
		goal := stored.Protocol()
		response.Goal = &goal
		if goal.TokenBudget != nil {
			remaining := max(*goal.TokenBudget-goal.TokensUsed, 0)
			response.RemainingTokens = &remaining
		}
		if completion && goal.Status == protocol.ThreadGoalComplete && (goal.TokenBudget != nil || goal.TimeUsedSeconds > 0) {
			response.CompletionBudgetReport = "Goal achieved. Report final token usage and elapsed time from this structured result when present."
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("encode goal result: %w", err)
	}
	return tool.ToolResult{ToolName: name, Text: string(encoded), Data: response}, nil
}

func validateBudget(value, maximum *int64) error {
	if value != nil && *value <= 0 {
		return errors.New("goal token budget must be positive")
	}
	if value != nil && maximum != nil && *value > *maximum {
		return fmt.Errorf("goal token budget %d exceeds the maximum allowed goal token budget of %d", *value, *maximum)
	}
	return nil
}

var _ tool.ToolDefinition = (*goalTool)(nil)
