package openai

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func reasoningEffort(request llm.Request) (llm.ReasoningEffort, bool, error) {
	if request.Reasoning == nil || request.Reasoning.Effort == nil {
		return "", false, nil
	}
	effort := *request.Reasoning.Effort
	if !effort.Valid() {
		return "", false, fmt.Errorf("reasoning effort %q is invalid", effort)
	}
	return effort, true, nil
}

func prepareStandardChatReasoning(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	effort, configured, err := reasoningEffort(request)
	if err != nil || !configured {
		return err
	}
	params.ReasoningEffort = shared.ReasoningEffort(effort)
	return nil
}

func prepareDeepSeekChatReasoning(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	return prepareChatReasoningWithDisabledThinking(request, params)
}

func prepareQwenChatReasoning(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	effort, configured, err := reasoningEffort(request)
	if err != nil || !configured {
		return err
	}
	if effort == llm.ReasoningEffortNone {
		params.SetExtraFields(map[string]any{"enable_thinking": false})
		return nil
	}
	params.ReasoningEffort = shared.ReasoningEffort(effort)
	return nil
}

func prepareGLMChatReasoning(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	return prepareChatReasoningWithDisabledThinking(request, params)
}

func prepareChatReasoningWithDisabledThinking(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	effort, configured, err := reasoningEffort(request)
	if err != nil || !configured {
		return err
	}
	if effort == llm.ReasoningEffortNone {
		params.SetExtraFields(map[string]any{"thinking": map[string]any{"type": "disabled"}})
		return nil
	}
	params.ReasoningEffort = shared.ReasoningEffort(effort)
	return nil
}
