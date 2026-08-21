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
	"github.com/Godric-W/Amadeus/internal/tool"
)

type compactFunc func(context.Context) (bool, error)

func (session *Session) continueTurn(ctx context.Context, runtime *SessionServices, modelSession *engine.ModelClientSession, turnContext turn.TurnContext, state *TurnState, events protocol.EventSink, compact compactFunc, canDrainPendingInput bool) (TaskOutput, error) {
	if runtime == nil || modelSession == nil || state == nil || events == nil {
		return TaskOutput{}, errors.New("session continuation is incomplete")
	}
	var usage llm.Usage
	toolCallCount := 0
	startedAt := time.Now()
	completionReminderSent := false
	budget := runtime.TurnBudget()
	modelContinuationPending := false
	for stepNumber := 1; ; stepNumber++ {
		if err := ctx.Err(); err != nil {
			return taskProgress(usage, toolCallCount), err
		}
		if reason := budget.Exhausted(stepNumber-1, toolCallCount, time.Since(startedAt)); reason != "" {
			return TaskOutput{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: blocked", Outcome: protocol.TurnOutcomeBlocked, Reason: reason}, nil
		}
		step, err := session.captureStep(ctx, runtime, turnContext)
		if err != nil {
			return taskProgress(usage, toolCallCount), err
		}
		if step.Prompt.NeedsCompaction(step.Model) && compact != nil {
			compacted, compactErr := compact(ctx)
			if compactErr != nil {
				return taskProgress(usage, toolCallCount), compactErr
			}
			if compacted {
				if modelContinuationPending {
					canDrainPendingInput = false
				}
				continue
			}
		}
		if canDrainPendingInput {
			pendingInput := session.inputQueue.Drain(state)
			if len(pendingInput) > 0 {
				for _, input := range pendingInput {
					userInput, ok := input.(UserTurnInput)
					if !ok {
						return taskProgress(usage, toolCallCount), fmt.Errorf("unsupported turn input %T", input)
					}
					if err := session.recordUserTurnInput(ctx, turnContext.TurnID, events, userInput); err != nil {
						return taskProgress(usage, toolCallCount), err
					}
					if err := runtime.prepareInputContext(ctx, userInput.Content, &turnContext, session.ContextUpdate, session.AppendItems); err != nil {
						return taskProgress(usage, toolCallCount), err
					}
				}
				step, err = session.captureStep(ctx, runtime, turnContext)
				if err != nil {
					return taskProgress(usage, toolCallCount), err
				}
			}
		}
		if err := events.Publish(ctx, protocol.Event{Msg: protocol.TokenCountEvent{
			EstimatedInputTokens: step.Prompt.Usage.EstimatedInputTokens,
			ContextWindow:        step.Model.ContextWindow,
		}}); err != nil {
			return taskProgress(usage, toolCallCount), fmt.Errorf("publish context usage: %w", err)
		}
		if !completionReminderSent && budget.Nearing(stepNumber-1, toolCallCount, time.Since(startedAt)) {
			step.Prompt.Items = append(step.Prompt.Items, llm.DeveloperMessage("The Turn is approaching its internal safety budget. Finish the highest-value remaining work now and provide a concise final response; do not start optional work."))
			completionReminderSent = true
		}
		sampleID := fmt.Sprintf("%s/step-%d", turnContext.TurnID, stepNumber)
		stepCtx := tool.WithInvocationMetadata(ctx, tool.InvocationMetadata{SessionID: string(turnContext.ThreadID), TurnID: string(turnContext.TurnID), Source: tool.ToolCallSourceModel})
		sampleEvents := events
		var proposedPlan *engine.ProposedPlanEventSink
		if turnContext.Mode == turn.ModeKindPlan {
			proposedPlan, err = engine.NewProposedPlanEventSink(events, protocol.ItemID(sampleID+":plan"))
			if err != nil {
				return taskProgress(usage, toolCallCount), err
			}
			sampleEvents = proposedPlan
		}
		sample, sampleErr := modelSession.Sample(stepCtx, engine.SampleRequest{
			ID: sampleID, Messages: step.Prompt.Items, BaseInstructions: step.BaseInstructions,
			Tools: step.ToolRouter.Specs(), OutputSchema: llm.OutputSchema(turnContext.OutputSchema), OutputSchemaStrict: turnContext.OutputSchemaStrict,
			Reasoning: llm.ReasoningConfigForEffort(turnContext.ReasoningEffort),
			Events:    sampleEvents,
		})
		usage = addUsage(usage, sample.Response.Usage)
		if sampleErr != nil {
			return taskProgress(usage, toolCallCount), sampleErr
		}
		if sample.Kind == engine.SampleFinal {
			if proposedPlan != nil {
				if err := proposedPlan.Flush(stepCtx); err != nil {
					return taskProgress(usage, toolCallCount), err
				}
				if err := engine.PersistAssistantResponse(stepCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, nil); err != nil {
					return taskProgress(usage, toolCallCount), err
				}
				if err := engine.PublishPlanModeCompletions(stepCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message, proposedPlan.AssistantText(), proposedPlan.PlanText()); err != nil {
					return taskProgress(usage, toolCallCount), err
				}
				if session.inputQueue.HasPending(state) {
					canDrainPendingInput = true
					modelContinuationPending = false
					continue
				}
				return TaskOutput{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
			}
			if err := engine.PersistAssistantResponse(stepCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, nil); err != nil {
				return taskProgress(usage, toolCallCount), err
			}
			if err := engine.PublishModelCompletions(stepCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message); err != nil {
				return taskProgress(usage, toolCallCount), err
			}
			if session.inputQueue.HasPending(state) {
				canDrainPendingInput = true
				modelContinuationPending = false
				continue
			}
			return TaskOutput{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
		}
		toolCallCount += len(sample.ToolCalls)
		observer := engine.NewToolEventObserver(session.AppendItems, turnContext.TurnID, events)
		recorded := false
		recorder := func(recordCtx context.Context, normalized []tool.ToolCall) error {
			recorded = true
			if err := engine.PersistAssistantResponse(recordCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, normalized); err != nil {
				return err
			}
			return engine.PublishModelCompletions(recordCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message)
		}
		_, err = runtime.ExecuteBatchScoped(stepCtx, sample.ToolCalls, recorder, tool.ExecutionScope{Observer: observer, Router: &step.ToolRouter})
		if err != nil {
			return taskProgress(usage, toolCallCount), err
		}
		if !recorded {
			return taskProgress(usage, toolCallCount), errors.New("tool execution did not record model response")
		}
		canDrainPendingInput = true
		modelContinuationPending = true
	}
}

func taskProgress(usage llm.Usage, toolCallCount int) TaskOutput {
	return TaskOutput{Usage: usage, ToolCallCount: toolCallCount}
}

func addUsage(total, next llm.Usage) llm.Usage {
	total.InputTokens += next.InputTokens
	total.CachedInputTokens += next.CachedInputTokens
	total.OutputTokens += next.OutputTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
	return total
}

func (session *Session) compactCallback(runtime *SessionServices, modelSession *engine.ModelClientSession, turnContext turn.TurnContext, events protocol.EventSink) compactFunc {
	return func(ctx context.Context) (bool, error) {
		items, err := runtime.Compact(ctx, engine.CompactRequest{
			History: session.ContextProjection(), ModelSession: modelSession,
			Reasoning: llm.ReasoningConfigForEffort(turnContext.ReasoningEffort), Events: events,
		})
		if err != nil {
			if strings.Contains(err.Error(), "no earlier turn") || strings.Contains(err.Error(), "no safely compactable") || strings.Contains(err.Error(), "no conversation") {
				return false, nil
			}
			return false, err
		}
		if len(items) == 0 {
			return false, nil
		}
		if err := session.AppendItems(ctx, turnContext.TurnID, items...); err != nil {
			return false, err
		}
		return true, nil
	}
}
