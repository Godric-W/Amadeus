package builtin

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type RequestUserInput struct{}

func NewRequestUserInput() *RequestUserInput { return &RequestUserInput{} }

func (*RequestUserInput) Spec() tool.ToolSpec { return requestUserInputSpec() }

func (*RequestUserInput) SupportsParallelToolCalls() bool { return false }

func (*RequestUserInput) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments tool.RequestUserInputArgs
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	return arguments.Validate()
}

func (*RequestUserInput) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments tool.RequestUserInputArgs
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}

func (*RequestUserInput) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(tool.RequestUserInputArgs)
	if !ok {
		return tool.ToolResult{}, errors.New("request_user_input preparation state is invalid")
	}
	if toolContext.Interactions == nil {
		return tool.ToolResult{}, tool.InteractionUnavailableError{Interaction: "request_user_input"}
	}
	response, err := toolContext.Interactions.RequestUserInput(toolContext.Context, prepared.Invocation.Call.ID, arguments)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("request user input: %w", err)
	}
	if err := response.Validate(arguments); err != nil {
		return tool.ToolResult{}, fmt.Errorf("validate user input response: %w", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("encode user input response: %w", err)
	}
	return tool.ToolResult{ToolName: "request_user_input", Text: string(encoded), Data: response}, nil
}

func requestUserInputSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name:        "request_user_input",
		Description: "Ask the user 1-3 concise questions when a material decision cannot be resolved from context. Prefer exploration and reasonable assumptions in default mode; use this in plan mode for choices that materially change the proposed plan. The interface adds an Other free-form choice automatically.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":3,"items":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$"},"header":{"type":"string","minLength":1},"question":{"type":"string","minLength":1},"options":{"type":"array","minItems":2,"maxItems":3,"items":{"type":"object","properties":{"label":{"type":"string","minLength":1},"description":{"type":"string","minLength":1}},"required":["label","description"],"additionalProperties":false}},"multi_select":{"type":"boolean"}},"required":["id","header","question","options"],"additionalProperties":false}}},"required":["questions"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNone,
		Idempotent:  false,
	}
}

var _ tool.ToolDefinition = (*RequestUserInput)(nil)
