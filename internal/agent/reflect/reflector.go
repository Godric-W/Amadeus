package reflector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type Options struct {
	Temperature     float64
	MaxOutputTokens int
}

type Reflector struct {
	client  llm.Client
	options Options
}

func New(client llm.Client, options Options) (*Reflector, error) {
	if client == nil {
		return nil, errors.New("reflector LLM client is nil")
	}
	if strings.TrimSpace(client.Model().Name) == "" {
		return nil, errors.New("reflector model is empty")
	}
	if options.Temperature < 0 || options.Temperature > 2 {
		return nil, errors.New("reflector temperature must be between 0 and 2")
	}
	if options.MaxOutputTokens <= 0 {
		return nil, errors.New("reflector max output tokens must be greater than zero")
	}
	return &Reflector{client: client, options: options}, nil
}

func (reflector *Reflector) Reflect(ctx context.Context, input engine.ReflectionInput) (engine.Reflection, error) {
	if err := input.Validate(); err != nil {
		return engine.Reflection{}, err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return engine.Reflection{}, fmt.Errorf("marshal reflection input: %w", err)
	}
	response, err := reflector.client.Complete(ctx, llm.Request{
		Model: reflector.client.Model().Name,
		Messages: []llm.Message{
			llm.SystemMessage(reflectionSystemPrompt),
			llm.UserMessage(string(payload)),
		},
		Temperature:     reflector.options.Temperature,
		MaxOutputTokens: reflector.options.MaxOutputTokens,
	})
	if err != nil {
		return engine.Reflection{}, err
	}
	if response.FinishReason != llm.FinishReasonStop {
		return engine.Reflection{}, fmt.Errorf("reflection response ended with finish reason %q", response.FinishReason)
	}
	result, err := decodeReflection(response.Message.Content)
	if err != nil {
		return engine.Reflection{}, err
	}
	if err := result.Validate(input.Verification); err != nil {
		return engine.Reflection{}, fmt.Errorf("validate reflection response: %w", err)
	}
	return result, nil
}

func decodeReflection(content string) (engine.Reflection, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(content)))
	decoder.DisallowUnknownFields()
	var result engine.Reflection
	if err := decoder.Decode(&result); err != nil {
		return engine.Reflection{}, fmt.Errorf("decode structured reflection: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return engine.Reflection{}, errors.New("decode structured reflection: multiple JSON values")
		}
		return engine.Reflection{}, fmt.Errorf("decode structured reflection trailing content: %w", err)
	}
	return result, nil
}

const reflectionSystemPrompt = `You are the Amadeus quality reflector. Return exactly one JSON object and no markdown or prose.
Allowed verdicts: accept, retry, replan, ask_user, abort.
Schema: {"scope":"task|run","verdict":"...","issues":[{"code":"...","summary":"...","severity":"info|warning|critical"}],"evidence_gaps":["..."],"next_action_hint":"...","plan_changes":[{"task_id":"...","description":"..."}],"lesson":"..."}.
Use accept only when deterministic verification passed. Do not include chain-of-thought; provide only concise issues, gaps, next action, optional plan changes, and a short lesson.`

var _ engine.Reflector = (*Reflector)(nil)
