package modelclient

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type streamChunkObserver func(context.Context, llm.StreamChunk, bool) error

type streamRead struct {
	chunk llm.StreamChunk
	err   error
}

func consumeResponseStream(ctx context.Context, stream llm.Stream, idleTimeout time.Duration, observe streamChunkObserver) (llm.Response, error) {
	response := llm.Response{Message: llm.AssistantMessage("")}
	reads := readResponseStream(ctx, stream)
	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()
	firstChunk := true

	for {
		select {
		case <-ctx.Done():
			return response, ctx.Err()
		case <-timer.C:
			return response, &llm.ProviderError{
				Kind:              llm.ProviderErrorTimeout,
				Message:           "idle timeout waiting for provider stream",
				AdditionalDetails: "idle timeout waiting for provider stream",
				Retryable:         true,
			}
		case result, ok := <-reads:
			if !ok {
				return response, &llm.ProviderError{
					Kind:              llm.ProviderErrorProtocol,
					Message:           "provider stream ended before model response completed",
					AdditionalDetails: "provider stream ended before model response completed",
					Retryable:         true,
				}
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return response, &llm.ProviderError{
						Kind:              llm.ProviderErrorProtocol,
						Message:           "provider stream ended before model response completed",
						AdditionalDetails: "provider stream ended before model response completed",
						Retryable:         true,
						Cause:             result.err,
					}
				}
				return response, result.err
			}
			resetTimer(timer, idleTimeout)
			if observe != nil {
				if err := observe(ctx, result.chunk, firstChunk); err != nil {
					return response, err
				}
			}
			firstChunk = false
			mergeStreamChunk(&response, result.chunk)
			if result.chunk.Completed() {
				return response, nil
			}
		}
	}
}

func readResponseStream(ctx context.Context, stream llm.Stream) <-chan streamRead {
	reads := make(chan streamRead, 1)
	go func() {
		defer close(reads)
		for {
			chunk, err := stream.Recv()
			select {
			case reads <- streamRead{chunk: chunk, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil || chunk.Completed() {
				return
			}
		}
	}()
	return reads
}

func mergeStreamChunk(response *llm.Response, chunk llm.StreamChunk) {
	if chunk.ID != "" {
		response.ID = chunk.ID
	}
	if chunk.RequestID != "" {
		response.RequestID = chunk.RequestID
	}
	response.Message.Content += chunk.ContentDelta
	response.Message.Reasoning += chunk.ReasoningDelta
	response.Message.ToolCalls = append(response.Message.ToolCalls, chunk.ToolCalls...)
	if chunk.TokenUsage != nil {
		response.TokenUsage = *chunk.TokenUsage
	}
	if chunk.Completed() {
		response.FinishReason = chunk.FinishReason
		response.ProviderFinishReason = chunk.ProviderFinishReason
	}
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}
