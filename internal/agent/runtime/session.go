package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type SessionOptions struct {
	Temperature     float64
	MaxOutputTokens int
}

type TurnInput struct {
	ID      string
	Content string
}

type Session struct {
	client  llm.Client
	events  event.Sink
	options SessionOptions
	mutex   sync.Mutex
	history []llm.Message
}

func NewSession(client llm.Client, events event.Sink, options SessionOptions) (*Session, error) {
	if client == nil {
		return nil, errors.New("session LLM client is nil")
	}
	if events == nil {
		return nil, errors.New("session event sink is nil")
	}
	if strings.TrimSpace(client.Model().Name) == "" {
		return nil, errors.New("session model is empty")
	}
	if options.Temperature < 0 || options.Temperature > 2 {
		return nil, errors.New("session temperature must be between 0 and 2")
	}
	if options.MaxOutputTokens <= 0 {
		return nil, errors.New("session max output tokens must be greater than zero")
	}
	return &Session{client: client, events: events, options: options}, nil
}

func (session *Session) RunTurn(ctx context.Context, input TurnInput) (llm.Response, error) {
	if strings.TrimSpace(input.ID) == "" {
		return llm.Response{}, errors.New("turn ID is empty")
	}
	if strings.TrimSpace(input.Content) == "" {
		return llm.Response{}, errors.New("turn content is empty")
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()

	if err := session.events.Publish(ctx, event.TurnStarted{
		TurnID: input.ID,
		Model:  session.client.Model(),
	}); err != nil {
		return llm.Response{}, fmt.Errorf("publish turn started: %w", err)
	}

	userMessage := llm.UserMessage(input.Content)
	messages := make([]llm.Message, 0, len(session.history)+1)
	messages = append(messages, session.history...)
	messages = append(messages, userMessage)
	request := llm.Request{
		Model:           session.client.Model().Name,
		Messages:        messages,
		Temperature:     session.options.Temperature,
		MaxOutputTokens: session.options.MaxOutputTokens,
	}
	stream, err := session.client.Stream(ctx, request)
	if err != nil {
		return llm.Response{}, session.failTurn(ctx, input.ID, err)
	}

	response, consumeErr := session.consumeStream(ctx, input.ID, stream)
	closeErr := stream.Close()
	if consumeErr != nil || closeErr != nil {
		combined := errors.Join(consumeErr, closeErr)
		return response, session.failTurn(ctx, input.ID, combined)
	}
	session.history = append(session.history, userMessage, response.Message)
	if err := session.events.Publish(ctx, event.TurnCompleted{
		TurnID:               input.ID,
		ResponseID:           response.ID,
		RequestID:            response.RequestID,
		FinishReason:         response.FinishReason,
		ProviderFinishReason: response.ProviderFinishReason,
	}); err != nil {
		return response, fmt.Errorf("publish turn completed: %w", err)
	}
	return response, nil
}

func (session *Session) consumeStream(ctx context.Context, turnID string, stream llm.Stream) (llm.Response, error) {
	response := llm.Response{Message: llm.AssistantMessage("")}
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return response, &llm.ProviderError{
					Kind:    llm.ProviderErrorProtocol,
					Message: "provider stream ended before completion",
				}
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
			if err := session.events.Publish(ctx, event.ReasoningDelta{
				TurnID:     turnID,
				ResponseID: response.ID,
				Delta:      chunk.ReasoningDelta,
			}); err != nil {
				return response, fmt.Errorf("publish reasoning delta: %w", err)
			}
		}
		if chunk.ContentDelta != "" {
			response.Message.Content += chunk.ContentDelta
			if err := session.events.Publish(ctx, event.TextDelta{
				TurnID:     turnID,
				ResponseID: response.ID,
				Delta:      chunk.ContentDelta,
			}); err != nil {
				return response, fmt.Errorf("publish text delta: %w", err)
			}
		}
		if chunk.Usage != nil {
			response.Usage = *chunk.Usage
			if err := session.events.Publish(ctx, event.UsageUpdated{
				TurnID:     turnID,
				ResponseID: response.ID,
				Usage:      response.Usage,
			}); err != nil {
				return response, fmt.Errorf("publish usage update: %w", err)
			}
		}
		if chunk.Completed() {
			response.FinishReason = chunk.FinishReason
			response.ProviderFinishReason = chunk.ProviderFinishReason
			return response, nil
		}
	}
}

func (session *Session) failTurn(ctx context.Context, turnID string, turnErr error) error {
	if turnErr == nil {
		return nil
	}
	publishErr := session.events.Publish(context.WithoutCancel(ctx), event.ErrorOccurred{
		TurnID: turnID,
		Error:  event.NewErrorInfo(turnErr),
	})
	if publishErr != nil {
		return errors.Join(turnErr, fmt.Errorf("publish turn error: %w", publishErr))
	}
	return turnErr
}
