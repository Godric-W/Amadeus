package goal

import (
	"context"
	"errors"
	"time"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type extensionConfig struct {
	Enabled            bool
	MaxGoalTokenBudget *int64
}

type runtimeAttachment struct{ Runtime *RuntimeHandle }

type Extension struct {
	extension.ThreadLifecycleDefaults
	extension.TurnLifecycleDefaults
	state   state.Runtime
	service *Service
	clock   func() time.Time
	events  extension.EventSink
}

func Install(builder *extension.Builder, runtime state.Runtime, service *Service, clock func() time.Time) (*Extension, error) {
	if builder == nil || runtime == nil || service == nil {
		return nil, errors.New("goal extension dependencies are incomplete")
	}
	if clock == nil {
		clock = time.Now
	}
	value := &Extension{state: runtime, service: service, clock: clock, events: builder.EventSink()}
	builder.ThreadLifecycle(value)
	builder.TurnLifecycle(value)
	builder.Config(value)
	builder.TokenUsage(value)
	builder.ToolLifecycle(value)
	builder.Tools(value)
	return value, nil
}

func (value *Extension) OnThreadStart(_ context.Context, input extension.ThreadStartInput) error {
	config := extensionConfig{Enabled: input.Config.Features.Goals, MaxGoalTokenBudget: cloneInt64(input.Config.Goals.MaxGoalTokenBudget)}
	extension.Set(input.ThreadData, config)
	automationValue, ok := extension.Get[agentsession.ThreadAutomation](input.ThreadData)
	if !ok || automationValue == nil || *automationValue == nil {
		return errors.New("goal thread automation is unavailable")
	}
	threadID, err := protocol.ParseThreadID(input.ThreadData.LevelID())
	if err != nil {
		return err
	}
	toolsAvailable := input.PersistentThreadStateAvailable && !input.SessionSource.IsSubAgent()
	accounting := newAccountingState(value.clock)
	runtime := newRuntimeHandle(threadID, value.state.Goals(), *automationValue, value.events, accounting, config.Enabled, toolsAvailable)
	extension.Set(input.ThreadData, runtimeAttachment{Runtime: runtime})
	value.service.register(runtime)
	return nil
}

func (value *Extension) OnThreadResume(ctx context.Context, input extension.ThreadResumeInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil {
		return nil
	}
	return runtime.restoreAfterResume(ctx)
}

func (value *Extension) OnThreadIdle(ctx context.Context, input extension.ThreadIdleInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil {
		return nil
	}
	return runtime.continueIfIdle(ctx)
}

func (value *Extension) OnThreadStop(_ context.Context, input extension.ThreadStopInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime != nil {
		value.service.unregister(runtime)
	}
	return nil
}

func (value *Extension) OnConfigChanged(_ context.Context, _ *extension.Data, threadData *extension.Data, _ config.Config, current config.Config) error {
	config := extensionConfig{Enabled: current.Features.Goals, MaxGoalTokenBudget: cloneInt64(current.Goals.MaxGoalTokenBudget)}
	extension.Set(threadData, config)
	if runtime := runtimeFromData(threadData); runtime != nil {
		runtime.setEnabled(config.Enabled)
	}
	return nil
}

func (value *Extension) OnTurnStart(ctx context.Context, input extension.TurnStartInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil || !runtime.enabled.Load() {
		return nil
	}
	if err := value.state.Goals().ClearContinuationDeferral(ctx, runtime.threadID); err != nil {
		return err
	}
	runtime.accounting.startTurn(input.TurnID, input.Mode, input.TokenUsageAtStart)
	if input.Mode == protocol.ModeKindPlan {
		runtime.accounting.clearCurrentGoal()
		return nil
	}
	goal, err := value.state.Goals().Get(ctx, runtime.threadID)
	if err != nil {
		return err
	}
	if goal != nil && (goal.Status == protocol.ThreadGoalActive || goal.Status == protocol.ThreadGoalBudgetLimited) {
		runtime.accounting.markTurnGoalActive(input.TurnID, goal.GoalID)
	}
	return nil
}

func (value *Extension) OnTurnStop(ctx context.Context, input extension.TurnStopInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil || !runtime.enabled.Load() {
		return nil
	}
	turnID := protocol.TurnID(input.TurnData.LevelID())
	_, err := runtime.accountTurnProgress(ctx, turnID, state.GoalAccountingActiveOnly, false, string(turnID)+":turn-stop")
	runtime.accounting.finishTurn(turnID)
	return err
}

func (value *Extension) OnTurnAbort(ctx context.Context, input extension.TurnAbortInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil || !runtime.enabled.Load() {
		return nil
	}
	turnID := protocol.TurnID(input.TurnData.LevelID())
	_, err := runtime.accountTurnProgress(ctx, turnID, state.GoalAccountingActiveOnly, false, string(turnID)+":turn-abort")
	runtime.accounting.finishTurn(turnID)
	return err
}

func (value *Extension) OnTurnError(ctx context.Context, input extension.TurnErrorInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil {
		return nil
	}
	return runtime.stopGoalForTurn(ctx, input.TurnID, input.Err)
}

func (value *Extension) OnTokenUsage(_ context.Context, input extension.TokenUsageInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime != nil && runtime.enabled.Load() {
		turnID := runtime.accounting.currentTurnID()
		runtime.accounting.recordUsage(turnID, input.Usage.TotalTokenUsage)
	}
	return nil
}

func (value *Extension) OnToolFinish(ctx context.Context, input extension.ToolFinishInput) error {
	runtime := runtimeFromData(input.ThreadData)
	if runtime == nil || !runtime.enabled.Load() || input.ToolName == updateGoalToolName {
		return nil
	}
	if input.Outcome != extension.ToolCallCompleted && input.Outcome != extension.ToolCallFailedAfterRun {
		return nil
	}
	goal, err := runtime.accountTurnProgress(ctx, input.TurnID, state.GoalAccountingActiveOnly, true, input.CallID)
	if err != nil || goal == nil || goal.Status != protocol.ThreadGoalBudgetLimited {
		return err
	}
	return runtime.injectBudgetLimit(ctx, *goal)
}

func (value *Extension) Tools(_, threadData, _ *extension.Data) []tool.ToolDefinition {
	runtime := runtimeFromData(threadData)
	if runtime == nil || !runtime.toolsVisible() {
		return nil
	}
	config, _ := extension.Get[extensionConfig](threadData)
	var maximum *int64
	if config != nil {
		maximum = cloneInt64(config.MaxGoalTokenBudget)
	}
	return []tool.ToolDefinition{
		newGoalTool(goalToolGet, runtime, maximum),
		newGoalTool(goalToolCreate, runtime, maximum),
		newGoalTool(goalToolUpdate, runtime, maximum),
	}
}

func runtimeFromData(data *extension.Data) *RuntimeHandle {
	attachment, ok := extension.Get[runtimeAttachment](data)
	if !ok || attachment == nil {
		return nil
	}
	return attachment.Runtime
}

var (
	_ extension.ThreadLifecycleContributor = (*Extension)(nil)
	_ extension.TurnLifecycleContributor   = (*Extension)(nil)
	_ extension.ConfigContributor          = (*Extension)(nil)
	_ extension.TokenUsageContributor      = (*Extension)(nil)
	_ extension.ToolLifecycleContributor   = (*Extension)(nil)
	_ extension.ToolContributor            = (*Extension)(nil)
)
