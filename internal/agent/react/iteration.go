package react

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

type IterationKind string

const (
	IterationCandidate IterationKind = "candidate"
	IterationToolCalls IterationKind = "tool_calls"
)

type IterationInput struct {
	ID                string
	Messages          []llm.Message
	BaseInstructions  llm.BaseInstructions
	AvailableTools    []tool.ToolSpec
	OutputSchema      llm.OutputSchema
	ParallelToolCalls bool
	Temperature       float64
	MaxOutputTokens   int
	Reasoning         *llm.ReasoningConfig
}

func (input IterationInput) Validate() error {
	if strings.TrimSpace(input.ID) == "" {
		return errors.New("model iteration ID is empty")
	}
	if len(input.Messages) == 0 {
		return errors.New("model iteration messages are empty")
	}
	if strings.TrimSpace(input.BaseInstructions.Text) == "" {
		return errors.New("model iteration BaseInstructions are empty")
	}
	if input.Temperature < 0 || input.Temperature > 2 {
		return errors.New("model iteration temperature must be between 0 and 2")
	}
	if input.MaxOutputTokens <= 0 {
		return errors.New("model iteration max output tokens must be greater than zero")
	}
	return nil
}

type IterationResult struct {
	Kind      IterationKind
	Response  llm.Response
	ToolCalls []tool.ToolCall
	Candidate *llm.Message
}

type ModelIterator interface {
	Run(context.Context, IterationInput) (IterationResult, error)
}

type Iterator struct {
	client llm.Client
	events protocol.EventSink
}

func NewIterator(client llm.Client, events protocol.EventSink) (*Iterator, error) {
	if client == nil {
		return nil, errors.New("model iterator LLM client is nil")
	}
	if events == nil {
		return nil, errors.New("model iterator event sink is nil")
	}
	if strings.TrimSpace(client.Model().Name) == "" {
		return nil, errors.New("model iterator model is empty")
	}
	return &Iterator{client: client, events: events}, nil
}

func (iterator *Iterator) Run(ctx context.Context, input IterationInput) (IterationResult, error) {
	if err := input.Validate(); err != nil {
		return IterationResult{}, err
	}
	definitions := toolDefinitions(input.AvailableTools)
	request := llm.Request{
		Model: iterator.client.Model().Name,
		Prompt: llm.Prompt{
			BaseInstructions: input.BaseInstructions,
			Input:            input.Messages, Tools: definitions,
			ParallelToolCalls: input.ParallelToolCalls, OutputSchema: input.OutputSchema,
		},
		Temperature:     input.Temperature,
		MaxOutputTokens: input.MaxOutputTokens,
		Reasoning:       input.Reasoning,
	}
	stream, err := iterator.client.Stream(ctx, request)
	if err != nil {
		return IterationResult{}, iterator.fail(ctx, input.ID, err)
	}

	response, consumeErr := iterator.consume(ctx, input.ID, stream)
	closeErr := stream.Close()
	if consumeErr != nil || closeErr != nil {
		combined := errors.Join(consumeErr, closeErr)
		return IterationResult{Response: response}, iterator.fail(ctx, input.ID, combined)
	}

	result, err := classify(response)
	if err != nil {
		return IterationResult{Response: response}, iterator.fail(ctx, input.ID, err)
	}
	return result, nil
}

func (iterator *Iterator) consume(ctx context.Context, iterationID string, stream llm.Stream) (llm.Response, error) {
	response := llm.Response{Message: llm.AssistantMessage("")}
	assistantItemID := inputItemID(iterationID, "assistant")
	reasoningItemID := inputItemID(iterationID, "reasoning")
	assistantStarted := false
	reasoningStarted := false
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return response, &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: "provider stream ended before model iteration completed"}
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
				if err := iterator.publishItemStarted(ctx, reasoningItemID, protocol.ItemReasoning); err != nil {
					return response, err
				}
				reasoningStarted = true
			}
			if err := iterator.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ReasoningDelta{ItemID: reasoningItemID, Delta: chunk.ReasoningDelta}}); err != nil {
				return response, fmt.Errorf("publish model reasoning delta: %w", err)
			}
		}
		if chunk.ContentDelta != "" {
			response.Message.Content += chunk.ContentDelta
			if !assistantStarted {
				if err := iterator.publishItemStarted(ctx, assistantItemID, protocol.ItemAssistantMessage); err != nil {
					return response, err
				}
				assistantStarted = true
			}
			if err := iterator.events.Publish(ctx, protocol.SessionEvent{Message: protocol.AssistantMessageDelta{ItemID: assistantItemID, Delta: chunk.ContentDelta}}); err != nil {
				return response, fmt.Errorf("publish model text delta: %w", err)
			}
		}
		if len(chunk.ToolCalls) != 0 {
			response.Message.ToolCalls = append(response.Message.ToolCalls, chunk.ToolCalls...)
		}
		if chunk.Usage != nil {
			response.Usage = *chunk.Usage
			if err := iterator.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ThreadTokenUsageUpdated{Usage: response.Usage}}); err != nil {
				return response, fmt.Errorf("publish model usage: %w", err)
			}
		}
		if chunk.Completed() {
			if assistantStarted {
				if err := iterator.publishItemCompleted(ctx, assistantItemID, protocol.ItemAssistantMessage, response.Message.Content); err != nil {
					return response, err
				}
			}
			if reasoningStarted {
				if err := iterator.publishItemCompleted(ctx, reasoningItemID, protocol.ItemReasoning, response.Message.Reasoning); err != nil {
					return response, err
				}
			}
			response.FinishReason = chunk.FinishReason
			response.ProviderFinishReason = chunk.ProviderFinishReason
			return response, nil
		}
	}
}

func classify(response llm.Response) (IterationResult, error) {
	if len(response.Message.ToolCalls) != 0 {
		calls := make([]tool.ToolCall, len(response.Message.ToolCalls))
		for index, call := range response.Message.ToolCalls {
			calls[index] = tool.NewCall(call.ID, call.Name, call.Arguments)
		}
		return IterationResult{Kind: IterationToolCalls, Response: response, ToolCalls: calls}, nil
	}
	if response.FinishReason == llm.FinishReasonToolCalls {
		return IterationResult{}, &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: "provider finished with tool_calls but returned no tool calls"}
	}
	if response.FinishReason != llm.FinishReasonStop {
		return IterationResult{}, fmt.Errorf("model iteration ended with unsupported finish reason %q", response.FinishReason)
	}
	if strings.TrimSpace(response.Message.Content) == "" {
		return IterationResult{}, &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: "model iteration returned neither text nor tool calls"}
	}
	candidate := response.Message
	return IterationResult{Kind: IterationCandidate, Response: response, Candidate: &candidate}, nil
}

func toolDefinitions(specs []tool.ToolSpec) []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, len(specs))
	for index, spec := range specs {
		definitions[index] = llm.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: append([]byte(nil), spec.InputSchema...),
		}
	}
	return definitions
}

func (iterator *Iterator) fail(ctx context.Context, iterationID string, iterationErr error) error {
	if iterationErr == nil {
		return nil
	}
	publishErr := iterator.events.Publish(context.WithoutCancel(ctx), protocol.SessionEvent{Message: protocol.StreamError{Error: iterationErr.Error()}})
	if publishErr != nil {
		return errors.Join(iterationErr, fmt.Errorf("publish model iteration error: %w", publishErr))
	}
	return iterationErr
}

func inputItemID(iterationID, kind string) string { return iterationID + ":" + kind }

func (iterator *Iterator) publishItemStarted(ctx context.Context, id string, kind protocol.ItemKind) error {
	now := time.Now().UTC()
	item := protocol.TurnItem{ID: id, Kind: kind, Status: protocol.ItemInProgress, CreatedAt: now}
	if err := iterator.events.Publish(ctx, protocol.SessionEvent{Message: protocol.ItemStarted{Item: item}}); err != nil {
		return fmt.Errorf("publish %s item start: %w", kind, err)
	}
	return nil
}

func (iterator *Iterator) publishItemCompleted(ctx context.Context, id string, kind protocol.ItemKind, text string) error {
	now := time.Now().UTC()
	item := protocol.TurnItem{ID: id, Kind: kind, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: text}
	if err := iterator.events.Publish(context.WithoutCancel(ctx), protocol.SessionEvent{Message: protocol.ItemCompleted{Item: item}}); err != nil {
		return fmt.Errorf("publish %s item completion: %w", kind, err)
	}
	return nil
}

var _ ModelIterator = (*Iterator)(nil)
