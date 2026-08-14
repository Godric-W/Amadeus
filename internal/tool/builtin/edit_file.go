package builtin

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type fileEditArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (toolImpl editTool) Spec() tool.ToolSpec             { return toolImpl.files.EditSpec() }
func (toolImpl editTool) SupportsParallelToolCalls() bool { return false }
func (toolImpl editTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var args fileEditArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return err
	}
	if args.OldString == args.NewString {
		return errors.New("edit old_string and new_string are identical")
	}
	return nil
}
func (toolImpl editTool) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return toolImpl.files.prepareEditChange(toolContext, invocation)
}
func (toolImpl editTool) Execute(toolContext tool.ToolUseContext, use tool.PreparedToolUse) (tool.ToolResult, error) {
	prepared, ok := use.State.(preparedChange)
	if !ok {
		return tool.ToolResult{}, errors.New("edit preparation state is invalid")
	}
	return toolImpl.files.applyPrepared(toolContext, use.Invocation, prepared)
}
