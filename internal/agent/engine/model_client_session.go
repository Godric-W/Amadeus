package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type SampleKind string

const (
	SampleFinal     SampleKind = "final"
	SampleToolCalls SampleKind = "tool_calls"
)

type SampleRequest struct {
	ID                 string
	Messages           []llm.ResponseItem
	BaseInstructions   llm.BaseInstructions
	Tools              []tool.ToolSpec
	OutputSchema       llm.OutputSchema
	OutputSchemaStrict bool
	Temperature        float64
	MaxOutputTokens    int
	Reasoning          *llm.ReasoningConfig
	Events             protocol.EventSink
}

type SampleResult struct {
	Kind      SampleKind
	Response  llm.Response
	ToolCalls []tool.ToolCall
}

type CompleteRequest struct {
	Request llm.Request
	Events  protocol.EventSink
}

type ModelClientSession struct {
	client            llm.Client
	streamIdleTimeout time.Duration
	retry             responseRetryPolicy
}

type ModelClientSessionConfig struct {
	StreamMaxRetries  int
	StreamIdleTimeout time.Duration

	backoff func(int) time.Duration
	sleep   func(context.Context, time.Duration) error
}

func NewModelClientSession(client llm.Client, config ModelClientSessionConfig) (*ModelClientSession, error) {
	if client == nil {
		return nil, errors.New("model client is nil")
	}
	if strings.TrimSpace(client.Model().Name) == "" {
		return nil, errors.New("model client model is empty")
	}
	if config.StreamMaxRetries < 0 {
		return nil, errors.New("model stream max retries must not be negative")
	}
	if config.StreamIdleTimeout <= 0 {
		return nil, errors.New("model stream idle timeout must be greater than zero")
	}
	retry := newResponseRetryPolicy(config.StreamMaxRetries)
	if config.backoff != nil {
		retry.backoff = config.backoff
	}
	if config.sleep != nil {
		retry.sleep = config.sleep
	}
	return &ModelClientSession{client: client, streamIdleTimeout: config.StreamIdleTimeout, retry: retry}, nil
}

func (session *ModelClientSession) Sample(ctx context.Context, request SampleRequest) (SampleResult, error) {
	if session == nil || session.client == nil {
		return SampleResult{}, errors.New("model client session is nil")
	}
	if strings.TrimSpace(request.ID) == "" || len(request.Messages) == 0 || strings.TrimSpace(request.BaseInstructions.Text) == "" {
		return SampleResult{}, errors.New("model sample request is incomplete")
	}
	if request.Events == nil {
		return SampleResult{}, errors.New("model sample event sink is nil")
	}
	if request.MaxOutputTokens <= 0 {
		return SampleResult{}, errors.New("model sample max output tokens must be greater than zero")
	}
	definitions := make([]llm.ToolSpec, len(request.Tools))
	for index, spec := range request.Tools {
		definitions[index] = llm.ToolSpec{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
	}
	modelRequest := llm.Request{
		Model: session.client.Model().Name,
		Prompt: llm.Prompt{
			BaseInstructions: request.BaseInstructions,
			Input:            request.Messages, Tools: definitions,
			ParallelToolCalls:  session.client.Model().SupportsParallelToolCalls,
			OutputSchema:       append(llm.OutputSchema(nil), request.OutputSchema...),
			OutputSchemaStrict: request.OutputSchemaStrict,
		},
		Temperature: request.Temperature, MaxOutputTokens: request.MaxOutputTokens, Reasoning: request.Reasoning,
	}
	projection := sampleStreamProjection{
		assistantID: request.ID + ":assistant",
		reasoningID: request.ID + ":reasoning",
		events:      request.Events,
	}
	response, err := session.runResponseStream(ctx, modelRequest, request.Events, func(attemptCtx context.Context, stream llm.Stream, retryCount int) (llm.Response, error) {
		return consumeResponseStream(attemptCtx, stream, session.streamIdleTimeout, func(chunkCtx context.Context, chunk llm.StreamChunk, firstChunk bool) error {
			return projection.observe(chunkCtx, chunk, firstChunk, retryCount > 0)
		})
	})
	if err != nil {
		return SampleResult{Response: response}, err
	}
	result, err := classifySample(response)
	if err != nil {
		providerError := normalizeResponseStreamError(err)
		publishErr := publishStreamFailure(context.WithoutCancel(ctx), request.Events, session.client.Model().Provider, providerError, false, providerError.Error())
		return SampleResult{Response: response}, errors.Join(providerError, publishErr)
	}
	return result, nil
}

func (session *ModelClientSession) Complete(ctx context.Context, request CompleteRequest) (llm.Response, error) {
	if session == nil || session.client == nil {
		return llm.Response{}, errors.New("model client session is nil")
	}
	if request.Events == nil {
		return llm.Response{}, errors.New("model completion event sink is nil")
	}
	if strings.TrimSpace(request.Request.Model) == "" {
		request.Request.Model = session.client.Model().Name
	}
	if strings.TrimSpace(request.Request.Model) == "" || strings.TrimSpace(request.Request.Prompt.BaseInstructions.Text) == "" || len(request.Request.Prompt.Input) == 0 {
		return llm.Response{}, errors.New("model completion request is incomplete")
	}
	return session.runResponseStream(ctx, request.Request, request.Events, func(attemptCtx context.Context, stream llm.Stream, _ int) (llm.Response, error) {
		return consumeResponseStream(attemptCtx, stream, session.streamIdleTimeout, nil)
	})
}

type sampleStreamProjection struct {
	assistantID      string
	reasoningID      string
	events           protocol.EventSink
	assistantStarted bool
	reasoningStarted bool
}

func (projection *sampleStreamProjection) observe(ctx context.Context, chunk llm.StreamChunk, firstChunk, retryAttempt bool) error {
	if firstChunk && retryAttempt {
		if projection.reasoningStarted {
			if err := projection.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ReasoningDelta{ItemID: projection.reasoningID, Reset: true}}); err != nil {
				return fmt.Errorf("reset model reasoning draft: %w", err)
			}
		}
		if projection.assistantStarted {
			if err := projection.events.Publish(ctx, protocol.SessionEvent{Message: protocol.AssistantMessageDelta{ItemID: projection.assistantID, Reset: true}}); err != nil {
				return fmt.Errorf("reset model text draft: %w", err)
			}
		}
	}
	if chunk.ReasoningDelta != "" {
		if !projection.reasoningStarted {
			if err := publishStreamItemStarted(ctx, projection.events, projection.reasoningID, protocol.ItemReasoning); err != nil {
				return err
			}
			projection.reasoningStarted = true
		}
		if err := projection.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ReasoningDelta{ItemID: projection.reasoningID, Delta: chunk.ReasoningDelta}}); err != nil {
			return fmt.Errorf("publish model reasoning delta: %w", err)
		}
	}
	if chunk.ContentDelta != "" {
		if !projection.assistantStarted {
			if err := publishStreamItemStarted(ctx, projection.events, projection.assistantID, protocol.ItemAssistantMessage); err != nil {
				return err
			}
			projection.assistantStarted = true
		}
		if err := projection.events.Publish(ctx, protocol.SessionEvent{Message: protocol.AssistantMessageDelta{ItemID: projection.assistantID, Delta: chunk.ContentDelta}}); err != nil {
			return fmt.Errorf("publish model text delta: %w", err)
		}
	}
	if chunk.Usage != nil {
		if err := projection.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ThreadTokenUsageUpdated{Usage: *chunk.Usage}}); err != nil {
			return fmt.Errorf("publish model usage: %w", err)
		}
	}
	return nil
}

func classifySample(response llm.Response) (SampleResult, error) {
	if len(response.Message.ToolCalls) > 0 {
		calls := make([]tool.ToolCall, len(response.Message.ToolCalls))
		for index, call := range response.Message.ToolCalls {
			calls[index] = tool.NewCall(call.ID, call.Name, call.Arguments)
		}
		return SampleResult{Kind: SampleToolCalls, Response: response, ToolCalls: calls}, nil
	}
	if response.FinishReason == llm.FinishReasonToolCalls {
		return SampleResult{}, &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: "provider finished with tool_calls but returned no tool calls"}
	}
	if response.FinishReason != llm.FinishReasonStop {
		return SampleResult{}, fmt.Errorf("model sample ended with unsupported finish reason %q", response.FinishReason)
	}
	if strings.TrimSpace(response.Message.Content) == "" {
		return SampleResult{}, &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: "model sample returned neither text nor tool calls"}
	}
	return SampleResult{Kind: SampleFinal, Response: response}, nil
}

func publishStreamItemStarted(ctx context.Context, events protocol.EventSink, id string, kind protocol.ItemKind) error {
	now := time.Now().UTC()
	return events.Publish(ctx, protocol.SessionEvent{Message: protocol.ItemStarted{Item: protocol.TurnItem{ID: id, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: now}}})
}
