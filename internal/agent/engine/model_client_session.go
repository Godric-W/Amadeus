package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	ID               string
	Messages         []llm.Message
	BaseInstructions llm.BaseInstructions
	Tools            []tool.ToolSpec
	OutputSchema     llm.OutputSchema
	Temperature      float64
	MaxOutputTokens  int
	Reasoning        *llm.ReasoningConfig
	Events           protocol.EventSink
}

type SampleResult struct {
	Kind      SampleKind
	Response  llm.Response
	ToolCalls []tool.ToolCall
}

type ModelClientSession struct {
	client llm.Client
}

func NewModelClientSession(client llm.Client) (*ModelClientSession, error) {
	if client == nil {
		return nil, errors.New("model client is nil")
	}
	if strings.TrimSpace(client.Model().Name) == "" {
		return nil, errors.New("model client model is empty")
	}
	return &ModelClientSession{client: client}, nil
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
	definitions := make([]llm.ToolDefinition, len(request.Tools))
	for index, spec := range request.Tools {
		definitions[index] = llm.ToolDefinition{Name: spec.Name, Description: spec.Description, InputSchema: append([]byte(nil), spec.InputSchema...)}
	}
	stream, err := session.client.Stream(ctx, llm.Request{
		Model: session.client.Model().Name,
		Prompt: llm.Prompt{
			BaseInstructions: request.BaseInstructions,
			Input:            request.Messages, Tools: definitions,
			ParallelToolCalls: session.client.Model().SupportsParallelToolCalls,
			OutputSchema:      append(llm.OutputSchema(nil), request.OutputSchema...),
		},
		Temperature: request.Temperature, MaxOutputTokens: request.MaxOutputTokens, Reasoning: request.Reasoning,
	})
	if err != nil {
		return SampleResult{}, publishSampleFailure(ctx, request.Events, err)
	}
	response, consumeErr := consumeStream(ctx, request.ID, request.Events, stream)
	closeErr := stream.Close()
	if consumeErr != nil || closeErr != nil {
		return SampleResult{Response: response}, publishSampleFailure(ctx, request.Events, errors.Join(consumeErr, closeErr))
	}
	result, err := classifySample(response)
	if err != nil {
		return SampleResult{Response: response}, publishSampleFailure(ctx, request.Events, err)
	}
	return result, nil
}

func consumeStream(ctx context.Context, sampleID string, events protocol.EventSink, stream llm.Stream) (llm.Response, error) {
	response := llm.Response{Message: llm.AssistantMessage("")}
	assistantID := sampleID + ":assistant"
	reasoningID := sampleID + ":reasoning"
	assistantStarted, reasoningStarted := false, false
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return response, &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: "provider stream ended before model sample completed"}
			}
			return response, err
		}
		if chunk.ID != "" {
			response.ID = chunk.ID
		}
		if chunk.RequestID != "" {
			response.RequestID = chunk.RequestID
		}
		if chunk.ReasoningDelta != "" {
			response.Message.Reasoning += chunk.ReasoningDelta
			if !reasoningStarted {
				if err := publishStreamItemStarted(ctx, events, reasoningID, protocol.ItemReasoning); err != nil {
					return response, err
				}
				reasoningStarted = true
			}
			if err := events.Publish(ctx, protocol.SessionEvent{Message: protocol.ReasoningDelta{ItemID: reasoningID, Delta: chunk.ReasoningDelta}}); err != nil {
				return response, fmt.Errorf("publish model reasoning delta: %w", err)
			}
		}
		if chunk.ContentDelta != "" {
			response.Message.Content += chunk.ContentDelta
			if !assistantStarted {
				if err := publishStreamItemStarted(ctx, events, assistantID, protocol.ItemAssistantMessage); err != nil {
					return response, err
				}
				assistantStarted = true
			}
			if err := events.Publish(ctx, protocol.SessionEvent{Message: protocol.AssistantMessageDelta{ItemID: assistantID, Delta: chunk.ContentDelta}}); err != nil {
				return response, fmt.Errorf("publish model text delta: %w", err)
			}
		}
		response.Message.ToolCalls = append(response.Message.ToolCalls, chunk.ToolCalls...)
		if chunk.Usage != nil {
			response.Usage = *chunk.Usage
			if err := events.Publish(ctx, protocol.SessionEvent{Message: protocol.ThreadTokenUsageUpdated{Usage: response.Usage}}); err != nil {
				return response, fmt.Errorf("publish model usage: %w", err)
			}
		}
		if !chunk.Completed() {
			continue
		}
		response.FinishReason = chunk.FinishReason
		response.ProviderFinishReason = chunk.ProviderFinishReason
		return response, nil
	}
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

func publishSampleFailure(ctx context.Context, events protocol.EventSink, sampleErr error) error {
	if sampleErr == nil {
		return nil
	}
	publishErr := events.Publish(context.WithoutCancel(ctx), protocol.SessionEvent{Message: protocol.StreamError{Error: sampleErr.Error()}})
	return errors.Join(sampleErr, publishErr)
}

func publishStreamItemStarted(ctx context.Context, events protocol.EventSink, id string, kind protocol.ItemKind) error {
	now := time.Now().UTC()
	return events.Publish(ctx, protocol.SessionEvent{Message: protocol.ItemStarted{Item: protocol.TurnItem{ID: id, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: now}}})
}
