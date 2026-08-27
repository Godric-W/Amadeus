package builtin

import (
	"crypto/sha256"
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
	target := tool.ContextTarget{Path: resolved.Canonical, Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectRead}
	return tool.PreparedToolUse{Invocation: invocation, Input: args, State: preparedRead{args: args, resolved: resolved}, Target: &target, Permission: tool.AllowPermission()}, nil
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
	info, err := os.Lstat(prepared.resolved.Canonical)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("stat read target: %w", err)
	}
	if sha256.Sum256(content) != result.ContentSHA256 || info.Size() != result.FileBytes {
		return tool.ToolResult{}, &staleFileError{Path: prepared.resolved.Canonical}
	}
	completeSnapshot := false
	if toolContext.FileReadState != nil {
		state, stateErr := tool.NewFileReadState(prepared.resolved.Canonical, content, info, false)
		if stateErr != nil {
			return tool.ToolResult{}, stateErr
		}
		if result.TotalLines == 0 && !result.OutputTruncated && result.LinesTruncated == 0 {
			state.FullRead = true
			toolContext.FileReadState.Record(state)
		} else if result.EndLine >= start && result.LinesTruncated == 0 {
			toolContext.FileReadState.RecordRange(state, start, result.EndLine, result.TotalLines)
		}
		if current, ok := toolContext.FileReadState.Get(prepared.resolved.Canonical); ok {
			completeSnapshot = current.FullRead && current.Matches(content, info)
		}
	}
	data := map[string]any{"path": prepared.resolved.Canonical, "start_line": result.StartLine, "end_line": result.EndLine, "total_lines": result.TotalLines, "truncated": result.Partial, "complete_snapshot": completeSnapshot}
	return tool.ToolResult{ToolName: "read", Text: result.Text, Partial: result.Partial, Data: data, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: prepared.args.Path}, Metadata: map[string]any{
		"path": prepared.resolved.Canonical, "content_hash": hashBytes(content), "start_line": result.StartLine,
		"end_line": result.EndLine, "next_line": result.NextLine, "lines_returned": result.LinesReturned,
		"total_lines": result.TotalLines, "bytes_returned": result.BytesReturned, "output_truncated": result.OutputTruncated,
		"lines_truncated": result.LinesTruncated, "complete_snapshot": completeSnapshot,
	}}, nil
}
