package openai

import (
	"context"
	"io"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
)

type chatCompletionChunkStream interface {
	Next() bool
	Current() openaisdk.ChatCompletionChunk
	Err() error
	Close() error
}

type chatCompletionsStream struct {
	stream            chatCompletionChunkStream
	pendingCompletion *llm.StreamChunk
	usage             *llm.Usage
}

func openChatCompletionsStream(ctx context.Context, client openaisdk.Client, request llm.Request) (llm.Stream, error) {
	params, err := newChatCompletionsRequest(request)
	if err != nil {
		return nil, err
	}
	params.StreamOptions = openaisdk.ChatCompletionStreamOptionsParam{
		IncludeUsage: openaisdk.Bool(true),
	}
	return &chatCompletionsStream{stream: client.Chat.Completions.NewStreaming(ctx, params)}, nil
}

func (stream *chatCompletionsStream) Recv() (llm.StreamChunk, error) {
	for stream.stream.Next() {
		chunk := stream.stream.Current()
		if chunk.JSON.Usage.Valid() {
			usage := chatCompletionsUsage(chunk.Usage)
			stream.usage = &usage
		}

		if len(chunk.Choices) == 0 {
			if stream.pendingCompletion != nil && stream.usage != nil {
				return stream.takeCompletion(), nil
			}
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			stream.pendingCompletion = &llm.StreamChunk{
				ID:                   chunk.ID,
				FinishReason:         chatCompletionsFinishReason(choice.FinishReason),
				ProviderFinishReason: choice.FinishReason,
			}
		}

		if choice.Delta.Content != "" {
			return llm.StreamChunk{
				ID:           chunk.ID,
				ContentDelta: choice.Delta.Content,
			}, nil
		}

		if stream.pendingCompletion != nil && stream.usage != nil {
			return stream.takeCompletion(), nil
		}
	}

	if err := stream.stream.Err(); err != nil {
		return llm.StreamChunk{}, normalizeProviderError(err)
	}
	if stream.pendingCompletion != nil {
		return stream.takeCompletion(), nil
	}
	return llm.StreamChunk{}, io.EOF
}

func (stream *chatCompletionsStream) takeCompletion() llm.StreamChunk {
	completion := *stream.pendingCompletion
	completion.Usage = stream.usage
	stream.pendingCompletion = nil
	stream.usage = nil
	return completion
}

func (stream *chatCompletionsStream) Close() error {
	return stream.stream.Close()
}

func chatCompletionsUsage(usage openaisdk.CompletionUsage) llm.Usage {
	return llm.Usage{
		InputTokens:       usage.PromptTokens,
		CachedInputTokens: usage.PromptTokensDetails.CachedTokens,
		OutputTokens:      usage.CompletionTokens,
		ReasoningTokens:   usage.CompletionTokensDetails.ReasoningTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

func chatCompletionsFinishReason(reason string) llm.FinishReason {
	switch reason {
	case "stop":
		return llm.FinishReasonStop
	case "length":
		return llm.FinishReasonLength
	case "tool_calls", "function_call":
		return llm.FinishReasonToolCalls
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonUnknown
	}
}

var _ llm.Stream = (*chatCompletionsStream)(nil)
