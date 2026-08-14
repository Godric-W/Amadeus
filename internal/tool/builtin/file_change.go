package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/filechange"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/textdiff"
)

type preparedChange struct {
	resolved  project.ResolvedPath
	before    []byte
	after     []byte
	change    filechange.Preview
	operation filechange.Operation
	readState tool.FileReadState
	mode      os.FileMode
}

func (files *FileTools) prepareEditChange(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var args fileEditArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.PreparedToolUse{}, err
	}
	if args.OldString == args.NewString {
		return tool.PreparedToolUse{}, errors.New("edit old_string and new_string are identical")
	}
	resolved, err := files.policy.ResolveExisting(args.Path, project.PathFile)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	before, err := os.ReadFile(resolved.Canonical)
	if err != nil {
		return tool.PreparedToolUse{}, fmt.Errorf("read edit target: %w", err)
	}
	info, err := os.Lstat(resolved.Canonical)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	readState, ok := toolContext.FileReadState.Get(resolved.Canonical)
	if !ok || !readState.FullRead || !readState.Matches(before, info) {
		return tool.PreparedToolUse{}, &readRequiredError{Path: resolved.Canonical}
	}
	count := strings.Count(string(before), args.OldString)
	if count == 0 {
		return tool.PreparedToolUse{}, fmt.Errorf("edit old_string was not found in %q", args.Path)
	}
	if count > 1 && !args.ReplaceAll {
		return tool.PreparedToolUse{}, fmt.Errorf("edit old_string matched %d times in %q; set replace_all or provide more context", count, args.Path)
	}
	after := []byte(strings.Replace(string(before), args.OldString, args.NewString, -1))
	return files.finalizePreparedChange(invocation, resolved, before, after, filechange.OperationUpdate, readState, info.Mode().Perm())
}

func (files *FileTools) prepareWriteChange(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var args fileWriteArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.PreparedToolUse{}, err
	}
	resolved, err := files.policy.ResolveForWriteTarget(args.Path)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	before, readErr := os.ReadFile(resolved.Canonical)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return tool.PreparedToolUse{}, readErr
	}
	operation := filechange.OperationUpdate
	var readState tool.FileReadState
	mode := files.fileMode
	if errors.Is(readErr, os.ErrNotExist) {
		before = nil
		operation = filechange.OperationCreate
	} else {
		info, statErr := os.Lstat(resolved.Canonical)
		if statErr != nil {
			return tool.PreparedToolUse{}, statErr
		}
		mode = info.Mode().Perm()
		var ok bool
		readState, ok = toolContext.FileReadState.Get(resolved.Canonical)
		if !ok || !readState.FullRead || !readState.Matches(before, info) {
			return tool.PreparedToolUse{}, &readRequiredError{Path: resolved.Canonical}
		}
	}
	return files.finalizePreparedChange(invocation, resolved, before, []byte(args.Content), operation, readState, mode)
}

func (files *FileTools) finalizePreparedChange(invocation tool.Invocation, resolved project.ResolvedPath, before, after []byte, operation filechange.Operation, readState tool.FileReadState, mode os.FileMode) (tool.PreparedToolUse, error) {
	change := buildFileChange(resolved.Canonical, before, after, operation)
	prepared := preparedChange{resolved: resolved, before: before, after: after, change: change, operation: operation, readState: readState, mode: mode}
	grant := policy.EditDirectoryGrant(filepath.Dir(resolved.Canonical))
	request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeFile, policy.CommandRiskModerate, policy.ApprovalCause{Kind: policy.ApprovalCauseFileChange, Code: string(operation), Detail: resolved.Canonical})
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	request.Path, request.Diff = resolved.Canonical, &change
	presentationOperation := "write-overwrite"
	if operation == filechange.OperationCreate {
		presentationOperation = "write-new"
	}
	request.Presentation = policy.FileApprovalPresentation(presentationOperation, resolved.Canonical, &change)
	target := tool.ContextTarget{Path: resolved.Canonical, Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectWrite}
	return tool.PreparedToolUse{Invocation: invocation, State: prepared, Target: &target, Permission: tool.PermissionEvaluation{Decision: tool.PermissionAsk, Request: &request, Grant: grant}}, nil
}

func (files *FileTools) applyPrepared(toolContext tool.ToolUseContext, invocation tool.Invocation, prepared preparedChange) (tool.ToolResult, error) {
	change := prepared.change
	if err := toolContext.Context.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	current, readErr := os.ReadFile(prepared.resolved.Canonical)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return tool.ToolResult{}, readErr
	}
	if prepared.operation == filechange.OperationUpdate && readErr != nil {
		return tool.ToolResult{}, &staleFileError{Path: prepared.resolved.Canonical}
	}
	if prepared.operation == filechange.OperationCreate && readErr == nil {
		return tool.ToolResult{}, &staleFileError{Path: prepared.resolved.Canonical}
	}
	if readErr == nil {
		info, statErr := os.Lstat(prepared.resolved.Canonical)
		if statErr != nil || !prepared.readState.Matches(current, info) || hashBytes(current) != change.BeforeHash {
			return tool.ToolResult{}, &staleFileError{Path: prepared.resolved.Canonical}
		}
	}
	if err := atomicWrite(prepared.resolved.Canonical, prepared.after, prepared.mode); err != nil {
		return tool.ToolResult{}, err
	}
	info, err := os.Lstat(prepared.resolved.Canonical)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("verify file change: %w", err)
	}
	if hashBytes(readFileBytes(prepared.resolved.Canonical)) != change.AfterHash {
		return tool.ToolResult{}, errors.New("verify file change: content hash mismatch")
	}
	state, err := tool.NewFileReadState(prepared.resolved.Canonical, prepared.after, info, true)
	if err != nil {
		return tool.ToolResult{}, err
	}
	toolContext.FileReadState.Record(state)
	beforeText, afterText := string(prepared.before), string(prepared.after)
	result := filechange.Result{Path: prepared.resolved.Canonical, Operation: prepared.operation, OriginalFile: &beforeText, UpdatedFile: &afterText, Diff: &change}
	return tool.ToolResult{ToolName: invocation.Call.Name, Text: fmt.Sprintf("updated %s", prepared.resolved.Canonical), Data: result, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayFileChange, Title: prepared.resolved.Canonical, Data: change}, Metadata: map[string]any{"change": change}}, nil
}

type readRequiredError struct{ Path string }

func (err *readRequiredError) Error() string {
	return fmt.Sprintf("read the complete file before modifying it: %s", err.Path)
}
func (err *readRequiredError) ToolErrorKind() string { return "read_required" }

type staleFileError struct{ Path string }

func (err *staleFileError) Error() string {
	return fmt.Sprintf("file changed since approval: %s", err.Path)
}
func (err *staleFileError) ToolErrorKind() string { return "target_stale" }

func buildFileChange(path string, before, after []byte, operation filechange.Operation) filechange.Preview {
	stats := textdiff.ChangedLineStats(before, after)
	diffOperation := "write"
	if operation == filechange.OperationCreate {
		diffOperation = "write-new"
	}
	unified := textdiff.ContentDiff(diffOperation, path, "", before, after)
	return filechange.Preview{Path: path, Operation: operation, BeforeHash: hashBytes(before), AfterHash: hashBytes(after), BeforeBytes: len(before), AfterBytes: len(after), UnifiedDiff: unified, Stats: filechange.DiffStats{Insertions: stats.Insertions, Deletions: stats.Deletions}, Hunks: []filechange.DiffHunk{{Header: path, Lines: strings.Split(strings.TrimSuffix(unified, "\n"), "\n")}}}
}

func hashBytes(value []byte) string    { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func readFileBytes(path string) []byte { value, _ := os.ReadFile(path); return value }

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".amadeus-write-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
