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
		return dialect{name: name, supportedWireAPIs: bothAPIs, capabilities: standardCapabilities, prepareChatRequest: prepareStandardChatReasoning, prepareChatMessage: prepareReasoningChatMessage, supportsStrictToolSchema: true}, nil
	case config.DialectOpenAI:
		return dialect{name: name, supportedWireAPIs: bothAPIs, capabilities: openAICapabilities, prepareChatRequest: prepareStandardChatReasoning, supportsStrictToolSchema: true}, nil
	case config.DialectDeepSeek:
		return dialect{name: name, supportedWireAPIs: bothAPIs, capabilities: reasoningCapabilities, prepareChatRequest: prepareDeepSeekChatReasoning, prepareChatMessage: prepareReasoningChatMessage}, nil
	case config.DialectQwen:
		return dialect{name: name, supportedWireAPIs: bothAPIs, capabilities: reasoningCapabilities, prepareChatRequest: prepareQwenChatReasoning, prepareChatMessage: prepareReasoningChatMessage}, nil
	case config.DialectGLM:
		return dialect{name: name, supportedWireAPIs: chatOnly, capabilities: reasoningCapabilities, prepareChatRequest: prepareGLMChatReasoning, prepareChatMessage: prepareReasoningChatMessage}, nil
	default:
		return nil, &DialectError{Dialect: name}
	}
}

func reasoningCapabilities(config.WireAPI) llm.Capabilities {
	return llm.Capabilities{
		SupportsStreaming:   true,
		SupportsReasoning:   true,
		SupportsStreamUsage: true,
	}
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
