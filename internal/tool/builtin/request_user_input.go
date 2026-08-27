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
		Description: "Request user input for one to three short questions and wait for the response. This tool is available to the root agent in Default or Plan mode.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":3,"description":"Questions to show the user. Prefer one and do not exceed three.","items":{"type":"object","properties":{"id":{"type":"string","pattern":"^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$","description":"Stable snake_case identifier used to map the answer."},"header":{"type":"string","minLength":1,"maxLength":12,"description":"Short header shown in the UI, no more than 12 characters."},"question":{"type":"string","minLength":1,"description":"Single-sentence prompt shown to the user."},"options":{"type":"array","minItems":2,"maxItems":3,"description":"Two or three mutually exclusive choices. Put the recommended option first and suffix its label with (Recommended). Do not add Other; the client adds it automatically.","items":{"type":"object","properties":{"label":{"type":"string","minLength":1,"description":"User-facing option label, ideally one to five words."},"description":{"type":"string","minLength":1,"description":"One short sentence explaining the impact or tradeoff."}},"required":["label","description"],"additionalProperties":false}},"multi_select":{"type":"boolean","description":"Allow selecting more than one listed option. Defaults to false."}},"required":["id","header","question","options"],"additionalProperties":false}}},"required":["questions"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNone,
		Idempotent:  false,
	}
}

var _ tool.ToolDefinition = (*RequestUserInput)(nil)
