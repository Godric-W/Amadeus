package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/modelclient"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func (session *Session) continueTurn(ctx context.Context, runtime *SessionServices, modelSession *modelclient.ModelClientSession, turnContext TurnContext, state *TurnState, events protocol.EventSink, canDrainPendingInput bool) (TaskOutput, error) {
	if runtime == nil || modelSession == nil || state == nil || events == nil {
		return TaskOutput{}, errors.New("session continuation is incomplete")
	}
	toolCallCount := 0
	startedAt := time.Now()
	completionReminderSent := false
	budget := runtime.TurnBudget()
	modelContinuationPending := false
	for stepNumber := 1; ; stepNumber++ {
		if err := ctx.Err(); err != nil {
			return taskProgress(toolCallCount), err
		}
		if reason := budget.Exhausted(stepNumber-1, toolCallCount, time.Since(startedAt)); reason != "" {
			return TaskOutput{ToolCallCount: toolCallCount, Summary: "result: blocked", Outcome: protocol.TurnOutcomeBlocked, Reason: reason}, nil
		}
		step, err := session.captureStep(ctx, runtime, turnContext)
		if err != nil {
			return taskProgress(toolCallCount), err
		}
		status, err := session.refreshContextWindowStatus(ctx, turnContext.TurnID, step, events)
		if err != nil {
			return taskProgress(toolCallCount), err
		}
		if status.TokenLimitReached {
			phase := protocol.CompactionPhasePreTurn
			if modelContinuationPending {
				phase = protocol.CompactionPhaseMidTurn
			}
			compacted, compactErr := session.runCompaction(ctx, runtime, modelSession, turnContext, &step, events, compactionInvocation{
				Trigger: protocol.CompactionTriggerAuto, Reason: protocol.CompactionReasonContextLimit, Phase: phase,
			})
			if compactErr != nil {
				return taskProgress(toolCallCount), compactErr
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
						return taskProgress(toolCallCount), fmt.Errorf("unsupported turn input %T", input)
					}
					if err := session.recordUserTurnInput(ctx, turnContext.TurnID, events, userInput); err != nil {
						return taskProgress(toolCallCount), err
					}
					if err := runtime.prepareInputContext(ctx, userInput.Content, &turnContext, session.ContextUpdate, session.AppendItems); err != nil {
						return taskProgress(toolCallCount), err
					}
				}
				step, err = session.captureStep(ctx, runtime, turnContext)
				if err != nil {
					return taskProgress(toolCallCount), err
				}
				status, err = session.refreshContextWindowStatus(ctx, turnContext.TurnID, step, events)
				if err != nil {
					return taskProgress(toolCallCount), err
				}
				if status.TokenLimitReached {
					compacted, compactErr := session.runCompaction(ctx, runtime, modelSession, turnContext, &step, events, compactionInvocation{
						Trigger: protocol.CompactionTriggerAuto, Reason: protocol.CompactionReasonContextLimit, Phase: protocol.CompactionPhaseMidTurn,
					})
					if compactErr != nil {
						return taskProgress(toolCallCount), compactErr
					}
					if compacted {
						if modelContinuationPending {
							canDrainPendingInput = false
						}
						continue
					}
				}
			}
		}
		if !completionReminderSent && budget.Nearing(stepNumber-1, toolCallCount, time.Since(startedAt)) {
			step.Prompt.Items = append(step.Prompt.Items, llm.DeveloperMessage("The Turn is approaching its internal safety budget. Finish the highest-value remaining work now and provide a concise final response; do not start optional work."))
			completionReminderSent = true
		}
		sampleID := fmt.Sprintf("%s/step-%d", turnContext.TurnID, stepNumber)
		stepCtx := tool.WithInvocationMetadata(ctx, tool.InvocationMetadata{
			SessionID: turnContext.SessionID, ThreadID: turnContext.ThreadID, TurnID: turnContext.TurnID, Source: tool.ToolCallSourceModel,
		})
		sampleEvents := events
		var proposedPlan *ProposedPlanEventSink
		if turnContext.Mode == ModeKindPlan {
			proposedPlan, err = NewProposedPlanEventSink(events, protocol.ItemID(sampleID+":plan"))
			if err != nil {
				return taskProgress(toolCallCount), err
			}
			sampleEvents = proposedPlan
		}
		sample, sampleErr := modelSession.Sample(stepCtx, modelclient.SampleRequest{
			ID: sampleID, Metadata: requestMetadata(turnContext), Messages: step.Prompt.Items, BaseInstructions: step.BaseInstructions,
			Tools: step.ToolRouter.Specs(), OutputSchema: llm.OutputSchema(turnContext.OutputSchema), OutputSchemaStrict: turnContext.OutputSchemaStrict,
			Reasoning: llm.ReasoningConfigForEffort(turnContext.ReasoningEffort),
			Events:    sampleEvents,
		})
		if sampleErr != nil {
			activeTokens := sample.Response.TokenUsage.InputTokens
			if err := session.recordTokenUsage(context.WithoutCancel(stepCtx), turnContext.TurnID, sample.Response.TokenUsage, activeTokens, step.Model.ContextWindow, step.Prompt.HistoryVersion, events); err != nil {
				return taskProgress(toolCallCount), errors.Join(sampleErr, err)
			}
			return taskProgress(toolCallCount), sampleErr
		}
		if sample.Kind == modelclient.SampleFinal {
			if proposedPlan != nil {
				if err := proposedPlan.Flush(stepCtx); err != nil {
					return taskProgress(toolCallCount), err
				}
				if err := persistAssistantResponse(stepCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, nil); err != nil {
					return taskProgress(toolCallCount), err
				}
				if err := publishPlanModeCompletions(stepCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message, proposedPlan.AssistantText(), proposedPlan.PlanText()); err != nil {
					return taskProgress(toolCallCount), err
				}
				if err := session.recordTokenUsage(stepCtx, turnContext.TurnID, sample.Response.TokenUsage, sample.Response.TokenUsage.TotalTokens, step.Model.ContextWindow, step.Prompt.HistoryVersion, events); err != nil {
					return taskProgress(toolCallCount), err
				}
				if session.inputQueue.HasPending(state) {
					canDrainPendingInput = true
					modelContinuationPending = false
					continue
				}
				return TaskOutput{ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
			}
			if err := persistAssistantResponse(stepCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, nil); err != nil {
				return taskProgress(toolCallCount), err
			}
			if err := publishModelCompletions(stepCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message); err != nil {
				return taskProgress(toolCallCount), err
			}
			if err := session.recordTokenUsage(stepCtx, turnContext.TurnID, sample.Response.TokenUsage, sample.Response.TokenUsage.TotalTokens, step.Model.ContextWindow, step.Prompt.HistoryVersion, events); err != nil {
				return taskProgress(toolCallCount), err
			}
			if session.inputQueue.HasPending(state) {
				canDrainPendingInput = true
				modelContinuationPending = false
				continue
			}
			return TaskOutput{ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
		}
		toolCallCount += len(sample.ToolCalls)
		observer := NewToolEventObserver(session.AppendItems, turnContext.ThreadID, turnContext.TurnID, events, runtime.resolveCollabAgentRef)
		recorded := false
		recorder := func(recordCtx context.Context, normalized []tool.ToolCall) error {
			recorded = true
			if err := persistAssistantResponse(recordCtx, session.AppendItems, turnContext.TurnID, sample.Response.Message, normalized); err != nil {
				return err
			}
			if err := publishModelCompletions(recordCtx, session.AppendItems, turnContext.TurnID, events, sampleID, sample.Response.Message); err != nil {
				return err
			}
			return session.recordTokenUsage(recordCtx, turnContext.TurnID, sample.Response.TokenUsage, sample.Response.TokenUsage.TotalTokens, step.Model.ContextWindow, step.Prompt.HistoryVersion, events)
		}
		_, err = runtime.ExecuteBatchScoped(stepCtx, sample.ToolCalls, recorder, tool.ExecutionScope{Observer: observer, Router: &step.ToolRouter})
		if err != nil {
			return taskProgress(toolCallCount), err
		}
		if !recorded {
			return taskProgress(toolCallCount), errors.New("tool execution did not record model response")
		}
		canDrainPendingInput = true
		modelContinuationPending = true
	}
}

func taskProgress(toolCallCount int) TaskOutput {
	return TaskOutput{ToolCallCount: toolCallCount}
}
