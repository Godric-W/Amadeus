package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type compactFunc func(context.Context) (bool, error)

func (session *Session) continueTurn(ctx context.Context, runtime *engine.Services, modelSession *engine.ModelClientSession, turnContext turn.TurnContext, events protocol.EventSink, instructions engine.StepInstructionScope, compact compactFunc, progress func(llm.Usage, int)) (engine.RunResult, error) {
	if runtime == nil || modelSession == nil || events == nil || instructions == nil {
		return engine.RunResult{}, errors.New("session continuation is incomplete")
	}
	var usage llm.Usage
	toolCallCount := 0
	startedAt := time.Now()
	completionReminderSent := false
	budget := runtime.TurnBudget()
	for stepNumber := 1; ; stepNumber++ {
		if err := ctx.Err(); err != nil {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: cancelled", Outcome: rollout.TurnOutcomeAborted, Reason: err.Error()}, err
		}
		if reason := budget.Exhausted(stepNumber-1, toolCallCount, time.Since(startedAt)); reason != "" {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: blocked", Outcome: rollout.TurnOutcomeBlocked, Reason: reason}, nil
		}
		step, err := runtime.CaptureStep(session.Snapshot, turnContext)
		if err != nil {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
		}
		if step.Prompt.NeedsCompaction(step.Model) && compact != nil {
			compacted, compactErr := compact(ctx)
			if compactErr != nil {
				return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, compactErr
			}
			if compacted {
				continue
			}
		}
		if err := events.Publish(ctx, protocol.SessionEvent{Message: protocol.ThreadTokenUsageUpdated{
			EstimatedInputTokens: step.Prompt.Usage.EstimatedInputTokens,
			ContextWindow:        step.Model.ContextWindow,
		}}); err != nil {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, fmt.Errorf("publish context usage: %w", err)
		}
		if !completionReminderSent && budget.Nearing(stepNumber-1, toolCallCount, time.Since(startedAt)) {
			step.Prompt.Items = append(step.Prompt.Items, llm.DeveloperMessage("The Turn is approaching its internal safety budget. Finish the highest-value remaining work now and provide a concise final response; do not start optional work."))
			completionReminderSent = true
		}
		sampleID := fmt.Sprintf("%s/step-%d", turnContext.TurnID, stepNumber)
		instructions.MarkSampled()
		stepCtx := tool.WithRequestSnapshot(ctx, step.RequestSnapshot)
		stepCtx = tool.WithInvocationMetadata(stepCtx, tool.InvocationMetadata{SessionID: string(turnContext.ThreadID), TurnID: string(turnContext.TurnID), Source: tool.ToolCallSourceModel})
		sample, sampleErr := modelSession.Sample(stepCtx, engine.SampleRequest{
			ID: sampleID, Messages: step.Prompt.Items, BaseInstructions: step.BaseInstructions,
			Tools: step.Tools, OutputSchema: llm.OutputSchema(turnContext.OutputSchema), OutputSchemaStrict: turnContext.OutputSchemaStrict,
			Temperature: runtime.ModelTemperature(), MaxOutputTokens: step.Model.MaxOutputTokens,
			Events: events,
		})
		usage = addUsage(usage, sample.Response.Usage)
		if progress != nil {
			progress(sample.Response.Usage, 0)
		}
		if sampleErr != nil {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, sampleErr
		}
		if sample.Kind == engine.SampleFinal {
			if err := engine.PersistAssistantResponse(stepCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, nil); err != nil {
				return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
			}
			if err := engine.PublishModelCompletions(stepCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message); err != nil {
				return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
			}
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: rollout.TurnOutcomeCompleted}, nil
		}
		toolCallCount += len(sample.ToolCalls)
		if progress != nil {
			progress(llm.Usage{}, len(sample.ToolCalls))
		}
		observer := engine.NewToolEventObserver(session.AppendItems, turnContext.TurnID, events)
		recorded := false
		recorder := func(recordCtx context.Context, normalized []tool.ToolCall) error {
			recorded = true
			if err := engine.PersistAssistantResponse(recordCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, normalized); err != nil {
				return err
			}
			return engine.PublishModelCompletions(recordCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message)
		}
		_, err = runtime.ExecuteBatchScoped(stepCtx, sample.ToolCalls, recorder, tool.ExecutionScope{Observer: observer, ContextScope: instructions, AllowedTools: step.ToolNames})
		if err != nil {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
		}
		if !recorded {
			return engine.RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, errors.New("tool execution did not record model response")
		}
	}
}

func addUsage(total, next llm.Usage) llm.Usage {
	total.InputTokens += next.InputTokens
	total.CachedInputTokens += next.CachedInputTokens
	total.OutputTokens += next.OutputTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
	return total
}

func (session *Session) runTurnLoop(ctx context.Context, runtime *engine.Services, turnContext turn.TurnContext, inputs []TurnInput, events protocol.EventSink, instructions engine.StepInstructionScope) (engine.RunResult, error) {
	if len(inputs) == 0 {
		return engine.RunResult{}, errors.New("session turn input is empty")
	}
	modelSession, err := runtime.NewModelClientSession()
	if err != nil {
		return engine.RunResult{}, err
	}
	compact := session.compactCallback(runtime, turnContext.TurnID)
	var progress *turn.TurnState
	if session.active != nil {
		progress = session.active.State
	}
	return session.continueTurn(ctx, runtime, modelSession, turnContext, events, instructions, compact, func(usage llm.Usage, tools int) {
		if progress != nil {
			progress.Record(usage, tools)
		}
	})
}

func (session *Session) compactCallback(runtime *engine.Services, turnID turn.ID) compactFunc {
	return func(ctx context.Context) (bool, error) {
		items, err := runtime.Compact(ctx, session.History())
		if err != nil {
			if strings.Contains(err.Error(), "no earlier turn") || strings.Contains(err.Error(), "no safely compactable") || strings.Contains(err.Error(), "no conversation") {
				return false, nil
			}
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		if err := session.AppendItems(ctx, turnID, items...); err != nil {
			return false, err
		}
		return true, nil
	}
}
