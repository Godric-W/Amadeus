package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type Services struct {
	providerName  string
	provider      config.ModelProviderInfo
	modelInfo     llm.ModelInfo
	client        llm.Client
	modelMessages llm.ModelMessages
	registry      *tool.Registry
	toolService   *tool.ToolExecutionService
	visibility    map[string]bool
	budget        TurnBudget
}

func (runtime *Services) NewModelClientSession() (*ModelClientSession, error) {
	if runtime == nil || runtime.client == nil {
		return nil, errors.New("test model client is unavailable")
	}
	return NewModelClientSession(runtime.client, ModelClientSessionConfig{
		StreamMaxRetries: runtime.provider.StreamMaxRetries, StreamIdleTimeout: runtime.provider.StreamIdleTimeout,
	})
}

func (runtime *Services) ModelInfo() llm.ModelInfo {
	if runtime == nil {
		return llm.ModelInfo{}
	}
	return runtime.modelInfo
}

func (runtime *Services) ModelMessages(model llm.ModelInfo) (llm.ModelMessages, error) {
	if model.ModelMessages.HasInstructions() {
		return model.ModelMessages.Normalized(), nil
	}
	if runtime != nil && runtime.modelMessages.HasInstructions() {
		return runtime.modelMessages.Normalized(), nil
	}
	return llm.ModelMessages{}, errors.New("test model messages are unavailable")
}

func (runtime *Services) CaptureStep(snapshot func(llm.ModelInfo, llm.Prompt) agentcontext.PromptSnapshot, turnContext turn.TurnContext) (StepContext, error) {
	if runtime == nil || snapshot == nil || runtime.registry == nil {
		return StepContext{}, errors.New("test step capture is incomplete")
	}
	entries := runtime.registry.VisibleSnapshot(runtime.visibility)
	tools := make([]tool.ToolSpec, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, entry.Spec.Clone())
	}
	if turnContext.Mode == turn.ModeKindPlan {
		tools = PlanModeTools(tools)
	}
	toolNames := make([]string, len(tools))
	definitions := make([]llm.ToolSpec, len(tools))
	toolSpecRevisions := make([]string, len(tools))
	for index, spec := range tools {
		toolNames[index] = spec.Name
		definitions[index] = llm.ToolSpec{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
		toolSpecRevisions[index] = definitions[index].RevisionID()
	}
	model := runtime.ModelInfo()
	messages, err := runtime.ModelMessages(model)
	if err != nil {
		return StepContext{}, err
	}
	base, err := messages.ResolveBaseInstructions(string(turnContext.Personality))
	if err != nil {
		return StepContext{}, err
	}
	prompt := llm.Prompt{BaseInstructions: base, Tools: definitions, ParallelToolCalls: model.SupportsParallelToolCalls, OutputSchema: append(llm.OutputSchema(nil), turnContext.OutputSchema...), OutputSchemaStrict: turnContext.OutputSchemaStrict}
	promptSnapshot := snapshot(model, prompt)
	requestSnapshot := tool.RequestSnapshot{ToolRevision: runtime.registry.Revision()}
	return StepContext{
		Turn: turnContext, Prompt: promptSnapshot, Model: model, BaseInstructions: base,
		Tools: cloneTestToolSpecs(tools), ToolNames: toolNames, ToolSpecRevisions: toolSpecRevisions,
		ToolRevision: requestSnapshot.ToolRevision, ModelMessagesRevision: messages.Revision,
		WorldStateRevision: promptSnapshot.WorldStateRevision, RequestSnapshot: requestSnapshot,
	}, nil
}

func cloneTestToolSpecs(specs []tool.ToolSpec) []tool.ToolSpec {
	cloned := make([]tool.ToolSpec, len(specs))
	for index, spec := range specs {
		cloned[index] = spec.Clone()
	}
	return cloned
}

type RunRequest struct {
	Snapshot     func(llm.ModelInfo, llm.Prompt) agentcontext.PromptSnapshot
	AppendItems  func(context.Context, protocol.TurnID, ...rollout.RolloutItem) error
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
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: cancelled", Outcome: protocol.TurnOutcomeAborted, Reason: err.Error()}, err
		}
		if reason := runtime.budget.Exhausted(stepNumber-1, toolCallCount, time.Since(startedAt)); reason != "" {
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: blocked", Outcome: protocol.TurnOutcomeBlocked, Reason: reason}, nil
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
		sample, sampleErr := modelSession.Sample(stepCtx, SampleRequest{ID: sampleID, Messages: step.Prompt.Items, BaseInstructions: step.BaseInstructions, Tools: step.Tools, OutputSchema: llm.OutputSchema(request.Turn.OutputSchema), OutputSchemaStrict: request.Turn.OutputSchemaStrict, Events: request.Events})
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
			return RunResult{Usage: usage, ToolCallCount: toolCallCount, Summary: "result: completed", Outcome: protocol.TurnOutcomeCompleted}, nil
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
