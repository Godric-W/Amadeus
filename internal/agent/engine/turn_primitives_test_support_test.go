package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RunRequest struct {
	Snapshot     func(llm.ModelInfo, llm.Prompt) agentcontext.PromptSnapshot
	AppendItems  func(context.Context, turn.ID, ...rollout.Item) error
	Progress     func(llm.Usage, int)
	ModelSession *ModelClientSession
	Turn         turn.TurnContext
	Events       protocol.EventSink
	Instructions StepInstructionScope
	Compact      func(context.Context) (bool, error)
}

func RunTurn(ctx context.Context, runtime *Services, request RunRequest) (RunResult, error) {
	if runtime == nil || request.Snapshot == nil || request.AppendItems == nil || request.Events == nil || request.Instructions == nil {
		return RunResult{}, errors.New("turn engine test request is incomplete")
	}
	modelSession := request.ModelSession
	if modelSession == nil {
		var err error
		modelSession, err = runtime.NewModelClientSession()
		if err != nil {
			return RunResult{}, err
		}
	}
	var usage llm.Usage
	toolCallCount := 0
	startedAt := time.Now()
	completionReminderSent := false
	for stepNumber := 1; ; stepNumber++ {
		if err := ctx.Err(); err != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: cancelled", Outcome: rollout.TurnOutcomeAborted, Reason: err.Error()}, err
		}
		if reason := runtime.budget.Exhausted(stepNumber-1, toolCallCount, time.Since(startedAt)); reason != "" {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: blocked", Outcome: rollout.TurnOutcomeBlocked, Reason: reason}, nil
		}
		step, err := runtime.CaptureStep(request.Snapshot, request.Turn)
		if err != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
		}
		if step.Prompt.NeedsCompaction(step.Model) && request.Compact != nil {
			compacted, compactErr := request.Compact(ctx)
			if compactErr != nil {
				return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, compactErr
			}
			if compacted {
				continue
			}
		}
		if !completionReminderSent && runtime.budget.Nearing(stepNumber-1, toolCallCount, time.Since(startedAt)) {
			step.Prompt.Items = append(step.Prompt.Items, llm.DeveloperMessage("The Turn is approaching its internal safety budget. Finish the highest-value remaining work now and provide a concise final response; do not start optional work."))
			completionReminderSent = true
		}
		sampleID := fmt.Sprintf("%s/step-%d", request.Turn.TurnID, stepNumber)
		request.Instructions.MarkSampled()
		stepCtx := tool.WithRequestSnapshot(ctx, step.RequestSnapshot)
		stepCtx = tool.WithInvocationMetadata(stepCtx, tool.InvocationMetadata{SessionID: string(request.Turn.ThreadID), TurnID: string(request.Turn.TurnID), Source: tool.ToolCallSourceModel})
		sample, sampleErr := modelSession.Sample(stepCtx, SampleRequest{ID: sampleID, Messages: step.Prompt.Items, BaseInstructions: step.BaseInstructions, Tools: step.Tools, OutputSchema: llm.OutputSchema(request.Turn.OutputSchema), OutputSchemaStrict: request.Turn.OutputSchemaStrict, Temperature: runtime.provider.Temperature, MaxOutputTokens: step.Model.MaxOutputTokens, Events: request.Events})
		usage = addUsage(usage, sample.Response.Usage)
		if request.Progress != nil {
			request.Progress(sample.Response.Usage, 0)
		}
		if sampleErr != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, sampleErr
		}
		if sample.Kind == SampleFinal {
			if err := PersistAssistantResponse(stepCtx, request.AppendItems, request.Turn.TurnID, sample.Response.Message, nil); err != nil {
				return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
			}
			if err := PublishModelCompletions(stepCtx, request.AppendItems, request.Turn.TurnID, request.Events, sampleID, sample.Response.Message); err != nil {
				return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
			}
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: rollout.TurnOutcomeCompleted}, nil
		}
		toolCallCount += len(sample.ToolCalls)
		if request.Progress != nil {
			request.Progress(llm.Usage{}, len(sample.ToolCalls))
		}
		observer := NewToolEventObserver(request.AppendItems, request.Turn.TurnID, request.Events)
		recorded := false
		recorder := func(recordCtx context.Context, normalized []tool.ToolCall) error {
			recorded = true
			if err := PersistAssistantResponse(recordCtx, request.AppendItems, request.Turn.TurnID, sample.Response.Message, normalized); err != nil {
				return err
			}
			return PublishModelCompletions(recordCtx, request.AppendItems, request.Turn.TurnID, request.Events, sampleID, sample.Response.Message)
		}
		_, err = runtime.toolService.ExecuteBatchScoped(stepCtx, sample.ToolCalls, recorder, tool.ExecutionScope{Observer: observer, ContextScope: request.Instructions, AllowedTools: step.ToolNames})
		if err != nil {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, err
		}
		if !recorded {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: failed"}, errors.New("tool execution did not record model response")
		}
	}
}
