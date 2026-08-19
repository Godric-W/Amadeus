package openai

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	openaisdk "github.com/openai/openai-go/v3"
)

type Dialect interface {
	Name() config.ProviderDialect
	SupportsWireAPI(config.WireAPI) bool
	Capabilities(config.WireAPI) llm.Capabilities
	PrepareChatRequest(llm.Request, *openaisdk.ChatCompletionNewParams) error
	PrepareChatMessage(llm.ResponseItem, *openaisdk.ChatCompletionMessageParamUnion) error
	SupportsStrictToolSchema() bool
}

type DialectError struct {
	Dialect config.ProviderDialect
	WireAPI config.WireAPI
	Reason  string
}

func (dialectError *DialectError) Error() string {
	if dialectError.WireAPI == "" {
		return fmt.Sprintf("provider dialect %q is unsupported", dialectError.Dialect)
	}
	return fmt.Sprintf(
		"provider dialect %q does not support wire API %q: %s",
		dialectError.Dialect,
		dialectError.WireAPI,
		dialectError.Reason,
	)
}

type dialect struct {
	name                     config.ProviderDialect
	supportedWireAPIs        map[config.WireAPI]struct{}
	capabilities             func(config.WireAPI) llm.Capabilities
	prepareChatRequest       func(llm.Request, *openaisdk.ChatCompletionNewParams) error
	prepareChatMessage       func(llm.ResponseItem, *openaisdk.ChatCompletionMessageParamUnion) error
	supportsStrictToolSchema bool
}

func (providerDialect dialect) Name() config.ProviderDialect {
	return providerDialect.name
}

func (providerDialect dialect) SupportsWireAPI(wireAPI config.WireAPI) bool {
	_, ok := providerDialect.supportedWireAPIs[wireAPI]
	return ok
}

func (providerDialect dialect) Capabilities(wireAPI config.WireAPI) llm.Capabilities {
	if !providerDialect.SupportsWireAPI(wireAPI) {
		return llm.Capabilities{}
	}
	return providerDialect.capabilities(wireAPI)
}

func (providerDialect dialect) PrepareChatRequest(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	if providerDialect.prepareChatRequest == nil {
		if request.Reasoning != nil {
			return fmt.Errorf("provider dialect %q does not support explicit reasoning configuration", providerDialect.name)
		}
		return nil
	}
	return providerDialect.prepareChatRequest(request, params)
}

func (providerDialect dialect) PrepareChatMessage(message llm.ResponseItem, converted *openaisdk.ChatCompletionMessageParamUnion) error {
	if providerDialect.prepareChatMessage == nil {
		return nil
	}
	return providerDialect.prepareChatMessage(message, converted)
}

func (providerDialect dialect) SupportsStrictToolSchema() bool {
	return providerDialect.supportsStrictToolSchema
}

func resolveDialect(name config.ProviderDialect) (Dialect, error) {
	standardCapabilities := func(config.WireAPI) llm.Capabilities {
		return llm.Capabilities{SupportsStreaming: true}
	}
	chatOnly := map[config.WireAPI]struct{}{
		config.WireAPIChatCompletions: {},
	}
	bothAPIs := map[config.WireAPI]struct{}{
		config.WireAPIResponses:       {},
		config.WireAPIChatCompletions: {},
	}

	switch name {
	case config.DialectStandard:
		return dialect{name: name, supportedWireAPIs: bothAPIs, capabilities: standardCapabilities, prepareChatMessage: prepareReasoningChatMessage, supportsStrictToolSchema: true}, nil
	case config.DialectOpenAI:
		return dialect{name: name, supportedWireAPIs: bothAPIs, capabilities: openAICapabilities, supportsStrictToolSchema: true}, nil
	case config.DialectDeepSeek:
		return dialect{name: name, supportedWireAPIs: chatOnly, capabilities: reasoningChatCapabilities, prepareChatMessage: prepareReasoningChatMessage}, nil
	case config.DialectQwen:
		return dialect{name: name, supportedWireAPIs: chatOnly, capabilities: reasoningChatCapabilities, prepareChatRequest: prepareQwenChatRequest}, nil
	case config.DialectGLM:
		return dialect{name: name, supportedWireAPIs: chatOnly, capabilities: reasoningChatCapabilities, prepareChatRequest: prepareGLMChatRequest, prepareChatMessage: prepareReasoningChatMessage}, nil
	default:
		return nil, &DialectError{Dialect: name}
	}
}

func reasoningChatCapabilities(config.WireAPI) llm.Capabilities {
	return llm.Capabilities{
		SupportsStreaming:   true,
		SupportsReasoning:   true,
		SupportsStreamUsage: true,
	}
}

func prepareQwenChatRequest(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	if request.Reasoning == nil {
		return nil
	}
	if request.Reasoning.Preserve != nil {
		return errorsForUnsupportedReasoningOption(config.DialectQwen, "preserve")
	}
	if request.Reasoning.Enabled != nil {
		params.SetExtraFields(map[string]any{"enable_thinking": *request.Reasoning.Enabled})
	}
	return nil
}

func prepareGLMChatRequest(request llm.Request, params *openaisdk.ChatCompletionNewParams) error {
	if request.Reasoning == nil {
		return nil
	}
	thinking := make(map[string]any)
	if request.Reasoning.Enabled != nil {
		if *request.Reasoning.Enabled {
			thinking["type"] = "enabled"
		} else {
			thinking["type"] = "disabled"
		}
	}
	if request.Reasoning.Preserve != nil {
		if request.Reasoning.Enabled != nil && !*request.Reasoning.Enabled && *request.Reasoning.Preserve {
			return fmt.Errorf("provider dialect %q cannot preserve reasoning while thinking is disabled", config.DialectGLM)
		}
		thinking["clear_thinking"] = !*request.Reasoning.Preserve
	}
	if len(thinking) != 0 {
		params.SetExtraFields(map[string]any{"thinking": thinking})
	}
	return nil
}

func prepareReasoningChatMessage(message llm.ResponseItem, converted *openaisdk.ChatCompletionMessageParamUnion) error {
	if message.Role != llm.RoleAssistant || message.Reasoning == "" {
		return nil
	}
	if converted.OfAssistant == nil {
		return fmt.Errorf("assistant reasoning history requires an assistant message")
	}
	converted.OfAssistant.SetExtraFields(map[string]any{"reasoning_content": message.Reasoning})
	return nil
}

func errorsForUnsupportedReasoningOption(dialect config.ProviderDialect, option string) error {
	return fmt.Errorf("provider dialect %q does not support reasoning option %q", dialect, option)
}

func openAICapabilities(wireAPI config.WireAPI) llm.Capabilities {
	capabilities := llm.Capabilities{SupportsStreaming: true, SupportsImages: true}
	if wireAPI == config.WireAPIResponses {
		capabilities.SupportsDeveloperRole = true
		capabilities.SupportsReasoning = true
		capabilities.SupportsStreamUsage = true
		capabilities.SupportsPromptCacheUsage = true
	}
	return capabilities
}
