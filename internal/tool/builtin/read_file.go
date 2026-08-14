package builtin

import (
	"errors"
	"fmt"
	"os"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type fileReadArgs struct {
	Path  string `json:"path"`
	Line  int    `json:"line,omitempty"`
	Limit int    `json:"limit,omitempty"`
}
type preparedRead struct {
	args     fileReadArgs
	resolved project.ResolvedPath
}

func (toolImpl readTool) Spec() tool.ToolSpec             { return toolImpl.files.ReadSpec() }
func (toolImpl readTool) SupportsParallelToolCalls() bool { return true }
func (toolImpl readTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var args fileReadArgs
	return decodeArguments(invocation.Call.Payload, &args)
}
func (toolImpl readTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var args fileReadArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.PreparedToolUse{}, err
	}
	resolved, err := toolImpl.files.policy.ResolveExisting(args.Path, project.PathFile)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: args, State: preparedRead{args: args, resolved: resolved}, Permission: tool.AllowPermission()}, nil
}
func (toolImpl readTool) Execute(toolContext tool.ToolUseContext, use tool.PreparedToolUse) (tool.ToolResult, error) {
	prepared, ok := use.State.(preparedRead)
	if !ok {
		return tool.ToolResult{}, errors.New("read preparation state is invalid")
	}
	return toolImpl.files.readPrepared(toolContext, prepared)
}

func (files *FileTools) readPrepared(toolContext tool.ToolUseContext, prepared preparedRead) (tool.ToolResult, error) {
	reader, err := workspace.NewReaderWithPolicy(files.root, files.policy)
	if err != nil {
		return tool.ToolResult{}, err
	}
	start := prepared.args.Line
	if start == 0 {
		start = 1
	}
	result, err := reader.ReadRangePrepared(toolContext.Context, prepared.resolved.Canonical, prepared.args.Path, workspace.ReadRangeOptions{StartLine: start, LineLimit: prepared.args.Limit, MaxBytes: int(files.maxBytes), MaxLineBytes: files.maxLineBytes, PrefixLines: true})
	if err != nil {
		return tool.ToolResult{}, err
	}
	content, err := os.ReadFile(prepared.resolved.Canonical)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("hash read target: %w", err)
	}
	if toolContext.FileReadState != nil && start == 1 && !result.Partial {
		info, statErr := os.Lstat(prepared.resolved.Canonical)
		if statErr != nil {
			return tool.ToolResult{}, fmt.Errorf("stat read target: %w", statErr)
		}
		state, stateErr := tool.NewFileReadState(prepared.resolved.Canonical, content, info, true)
		if stateErr != nil {
			return tool.ToolResult{}, stateErr
		}
		toolContext.FileReadState.Record(state)
	}
	data := map[string]any{"path": prepared.resolved.Canonical, "start_line": result.StartLine, "end_line": result.EndLine, "total_lines": result.TotalLines, "truncated": result.Partial}
	return tool.ToolResult{ToolName: "read", Text: result.Text, Partial: result.Partial, Data: data, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: prepared.args.Path}, Metadata: map[string]any{
		"path": prepared.resolved.Canonical, "content_hash": hashBytes(content), "start_line": result.StartLine,
		"end_line": result.EndLine, "next_line": result.NextLine, "lines_returned": result.LinesReturned,
		"total_lines": result.TotalLines, "bytes_returned": result.BytesReturned, "output_truncated": result.OutputTruncated,
	}}, nil
}
