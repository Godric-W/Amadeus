package openai

import (
	"context"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type responseEventStream interface {
	Next() bool
	Current() responses.ResponseStreamEventUnion
	Err() error
	Close() error
}

type responsesStream struct {
	stream     responseEventStream
	responseID string
	toolCalls  toolCallAggregator
}

func openResponsesStream(ctx context.Context, client openaisdk.Client, request llm.Request) (llm.Stream, error) {
	params, err := newResponsesRequest(request)
	if err != nil {
		return nil, err
	}
	return &responsesStream{stream: client.Responses.NewStreaming(ctx, params)}, nil
}

func (stream *responsesStream) Recv() (llm.StreamChunk, error) {
	for stream.stream.Next() {
		event := stream.stream.Current()
		switch event.Type {
		case "response.created":
			stream.responseID = event.AsResponseCreated().Response.ID
		case "response.in_progress":
			stream.responseID = event.AsResponseInProgress().Response.ID
		case "response.output_text.delta":
			return llm.StreamChunk{
				ID:           stream.responseID,
				ContentDelta: event.AsResponseOutputTextDelta().Delta,
			}, nil
		case "response.reasoning_summary_text.delta":
			return llm.StreamChunk{
				ID:             stream.responseID,
				ReasoningDelta: event.AsResponseReasoningSummaryTextDelta().Delta,
			}, nil
		case "response.reasoning_text.delta":
			return llm.StreamChunk{
				ID:             stream.responseID,
				ReasoningDelta: event.AsResponseReasoningTextDelta().Delta,
			}, nil
		case "response.output_item.added":
			added := event.AsResponseOutputItemAdded()
			if added.Item.Type == "function_call" {
				call := added.Item.AsFunctionCall()
				if err := stream.toolCalls.add(added.Item.ID, added.OutputIndex, call.CallID, call.Name, call.Arguments); err != nil {
					return llm.StreamChunk{}, err
				}
			}
		case "response.function_call_arguments.delta":
			delta := event.AsResponseFunctionCallArgumentsDelta()
			if err := stream.toolCalls.add(delta.ItemID, delta.OutputIndex, "", "", delta.Delta); err != nil {
				return llm.StreamChunk{}, err
			}
		case "response.function_call_arguments.done":
			done := event.AsResponseFunctionCallArgumentsDone()
			if err := stream.toolCalls.replaceArguments(done.ItemID, done.Name, done.Arguments); err != nil {
				return llm.StreamChunk{}, err
			}
		case "response.completed":
			response := event.AsResponseCompleted().Response
			stream.responseID = response.ID
			usage := responsesUsage(response.Usage)
			toolCalls, err := stream.toolCalls.finalize()
			if err != nil {
				return llm.StreamChunk{}, err
			}
			finishReason := llm.FinishReasonStop
			if len(toolCalls) != 0 {
				finishReason = llm.FinishReasonToolCalls
			}
			return llm.StreamChunk{
				ID:                   response.ID,
				FinishReason:         finishReason,
				ProviderFinishReason: string(response.Status),
				Usage:                &usage,
				ToolCalls:            toolCalls,
			}, nil
		case "response.incomplete":
			response := event.AsResponseIncomplete().Response
			stream.responseID = response.ID
			usage := responsesUsage(response.Usage)
			return llm.StreamChunk{
				ID:                   response.ID,
				FinishReason:         incompleteFinishReason(response.IncompleteDetails.Reason),
				ProviderFinishReason: incompleteProviderReason(response),
				Usage:                &usage,
			}, nil
		case "response.failed":
			response := event.AsResponseFailed().Response
			return llm.StreamChunk{}, responseFailure(response)
		case "error":
			responseError := event.AsError()
			return llm.StreamChunk{}, newProviderError(
				providerErrorKind(0, responseError.Code),
				0,
				responseError.Code,
				responseError.Param,
				responseError.Message,
				"",
				nil,
			)
		}
	}

	if err := stream.stream.Err(); err != nil {
		return llm.StreamChunk{}, normalizeProviderError(err)
	}
	return llm.StreamChunk{}, io.EOF
}

func (stream *responsesStream) Close() error {
	return stream.stream.Close()
}

func responsesUsage(usage responses.ResponseUsage) llm.Usage {
	return llm.Usage{
		InputTokens:       usage.InputTokens,
		CachedInputTokens: usage.InputTokensDetails.CachedTokens,
		OutputTokens:      usage.OutputTokens,
		ReasoningTokens:   usage.OutputTokensDetails.ReasoningTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

func incompleteFinishReason(reason string) llm.FinishReason {
	switch reason {
	case "max_output_tokens":
		return llm.FinishReasonLength
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonUnknown
	}
}

func incompleteProviderReason(response responses.Response) string {
	if reason := strings.TrimSpace(response.IncompleteDetails.Reason); reason != "" {
		return reason
	}
	return string(response.Status)
}

func responseFailure(response responses.Response) error {
	message := strings.TrimSpace(response.Error.Message)
	if message == "" {
		message = "response failed"
	}
	code := string(response.Error.Code)
	return newProviderError(providerErrorKind(0, code), 0, code, "", message, response.ID, nil)
}

var _ llm.Stream = (*responsesStream)(nil)
