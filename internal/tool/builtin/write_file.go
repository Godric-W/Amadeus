package builtin

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type fileWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (toolImpl writeTool) Spec() tool.ToolSpec             { return toolImpl.files.WriteSpec() }
func (toolImpl writeTool) SupportsParallelToolCalls() bool { return false }
func (toolImpl writeTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var args fileWriteArgs
	return decodeArguments(invocation.Call.Payload, &args)
}
func (toolImpl writeTool) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	return toolImpl.files.prepareWriteChange(toolContext, invocation)
}
func (toolImpl writeTool) Execute(toolContext tool.ToolUseContext, use tool.PreparedToolUse) (tool.ToolResult, error) {
	prepared, ok := use.State.(preparedChange)
	if !ok {
		return tool.ToolResult{}, errors.New("write preparation state is invalid")
	}
	return toolImpl.files.applyPrepared(toolContext, use.Invocation, prepared)
}
