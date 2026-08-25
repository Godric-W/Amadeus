package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Godric-W/Amadeus/internal/config"
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
	usage             *llm.TokenUsage
	toolCalls         toolCallAggregator
	dialect           Dialect
}

func openChatCompletionsStream(ctx context.Context, client openaisdk.Client, request llm.Request) (llm.Stream, error) {
	standard, err := resolveDialect(config.DialectStandard)
	if err != nil {
		return nil, err
	}
	return openChatCompletionsStreamForDialect(ctx, client, request, standard)
}

func openChatCompletionsStreamForDialect(ctx context.Context, client openaisdk.Client, request llm.Request, dialect Dialect) (llm.Stream, error) {
	params, err := newChatCompletionsRequestForDialect(request, dialect)
	if err != nil {
		return nil, err
	}
	params.StreamOptions = openaisdk.ChatCompletionStreamOptionsParam{
		IncludeUsage: openaisdk.Bool(true),
	}
	return &chatCompletionsStream{stream: client.Chat.Completions.NewStreaming(ctx, params), dialect: dialect}, nil
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
		for _, call := range choice.Delta.ToolCalls {
			key := fmt.Sprintf("%d", call.Index)
			if err := stream.toolCalls.add(key, call.Index, call.ID, call.Function.Name, call.Function.Arguments); err != nil {
				return llm.StreamChunk{}, err
			}
		}
		reasoningDelta, err := chatReasoningDelta(stream.dialect, choice.Delta.RawJSON())
		if err != nil {
			return llm.StreamChunk{}, err
		}
		if reasoningDelta != "" {
			return llm.StreamChunk{ID: chunk.ID, ReasoningDelta: reasoningDelta}, nil
		}
		if choice.FinishReason != "" {
			toolCalls, err := stream.toolCalls.finalize()
			if err != nil {
				return llm.StreamChunk{}, err
			}
			finishReason := chatCompletionsFinishReason(choice.FinishReason)
			if len(toolCalls) != 0 {
				finishReason = llm.FinishReasonToolCalls
			}
			stream.pendingCompletion = &llm.StreamChunk{
				ID:                   chunk.ID,
				FinishReason:         finishReason,
				ProviderFinishReason: choice.FinishReason,
				ToolCalls:            toolCalls,
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

func chatReasoningDelta(dialect Dialect, raw string) (string, error) {
	if dialect == nil {
		return "", nil
	}
	switch dialect.Name() {
	case config.DialectDeepSeek, config.DialectQwen, config.DialectGLM:
	default:
		return "", nil
	}
	var extension struct {
		ReasoningContent json.RawMessage `json:"reasoning_content"`
	}
	if err := json.Unmarshal([]byte(raw), &extension); err != nil {
		return "", protocolToolCallError("chat delta is invalid JSON")
	}
	if len(extension.ReasoningContent) == 0 || string(extension.ReasoningContent) == "null" {
		return "", nil
	}
	var delta string
	if err := json.Unmarshal(extension.ReasoningContent, &delta); err != nil {
		return "", protocolToolCallError("reasoning_content delta is not a JSON string")
	}
	return delta, nil
}

func (stream *chatCompletionsStream) takeCompletion() llm.StreamChunk {
	completion := *stream.pendingCompletion
	completion.TokenUsage = stream.usage
	stream.pendingCompletion = nil
	stream.usage = nil
	return completion
}

func (stream *chatCompletionsStream) Close() error {
	return stream.stream.Close()
}

func chatCompletionsUsage(usage openaisdk.CompletionUsage) llm.TokenUsage {
	return llm.TokenUsage{
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
