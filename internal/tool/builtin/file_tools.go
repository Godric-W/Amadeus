package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/tool/textdiff"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type FileToolsOptions struct {
	FileSystemPolicy *project.FileSystemPolicy
	MaxBytes         int64
	MaxLineBytes     int
	FileMode         os.FileMode
}

type FileTools struct {
	policy       *project.FileSystemPolicy
	maxBytes     int64
	maxLineBytes int
	fileMode     os.FileMode
	root         project.Root
}

type fileReadArgs struct {
	Path  string `json:"path"`
	Line  int    `json:"line,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type fileEditArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type fileWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type preparedRead struct {
	args     fileReadArgs
	resolved project.ResolvedPath
}

type preparedChange struct {
	resolved  project.ResolvedPath
	before    []byte
	after     []byte
	change    FileChange
	operation string
}

type FileChange struct {
	Path        string `json:"path"`
	Operation   string `json:"operation"`
	BeforeHash  string `json:"before_hash,omitempty"`
	AfterHash   string `json:"after_hash"`
	Insertions  int    `json:"insertions"`
	Deletions   int    `json:"deletions"`
	BeforeBytes int    `json:"before_bytes"`
	AfterBytes  int    `json:"after_bytes"`
	UnifiedDiff string `json:"unified_diff"`
}

func NewFileTools(root project.Root, options FileToolsOptions) (*FileTools, error) {
	if root.Path() == "" {
		return nil, errors.New("file tools project root is empty")
	}
	if options.FileSystemPolicy == nil {
		var err error
		options.FileSystemPolicy, err = project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
			CWD: root.Path(), Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{root.Path()}},
		})
		if err != nil {
			return nil, err
		}
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 2 << 20
	}
	if options.MaxLineBytes <= 0 {
		options.MaxLineBytes = 32 << 10
	}
	if options.FileMode == 0 {
		options.FileMode = 0o644
	}
	return &FileTools{root: root, policy: options.FileSystemPolicy, maxBytes: options.MaxBytes,
		maxLineBytes: options.MaxLineBytes, fileMode: options.FileMode}, nil
}

func (files *FileTools) ReadSpec() tool.ToolSpec {
	return readSpec()
}

func (files *FileTools) EditSpec() tool.ToolSpec {
	return editSpec()
}

func (files *FileTools) WriteSpec() tool.ToolSpec {
	return writeSpec()
}

type readTool struct{ files *FileTools }
type editTool struct{ files *FileTools }
type writeTool struct{ files *FileTools }

func (files *FileTools) ReadTool() tool.Tool  { return readTool{files: files} }
func (files *FileTools) EditTool() tool.Tool  { return editTool{files: files} }
func (files *FileTools) WriteTool() tool.Tool { return writeTool{files: files} }

func (toolImpl readTool) Spec() tool.ToolSpec             { return toolImpl.files.ReadSpec() }
func (toolImpl readTool) SupportsParallelToolCalls() bool { return true }
func (toolImpl readTool) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	check, ok := tool.PermissionCheckFromContext(ctx)
	if !ok {
		return tool.Output{}, errors.New("read must execute through ToolExecutionService")
	}
	prepared, ok := check.Prepared.(preparedRead)
	if !ok {
		return tool.Output{}, errors.New("read permission preparation is missing")
	}
	return toolImpl.files.readPrepared(ctx, prepared)
}

func (toolImpl editTool) Spec() tool.ToolSpec             { return toolImpl.files.EditSpec() }
func (toolImpl editTool) SupportsParallelToolCalls() bool { return false }
func (toolImpl editTool) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	check, ok := tool.PermissionCheckFromContext(ctx)
	if !ok {
		return tool.Output{}, errors.New("edit must execute through ToolExecutionService")
	}
	prepared, ok := check.Prepared.(preparedChange)
	if !ok {
		return tool.Output{}, errors.New("edit permission preparation is missing")
	}
	return toolImpl.files.applyPrepared(ctx, invocation, prepared)
}

func (toolImpl writeTool) Spec() tool.ToolSpec             { return toolImpl.files.WriteSpec() }
func (toolImpl writeTool) SupportsParallelToolCalls() bool { return false }
func (toolImpl writeTool) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	check, ok := tool.PermissionCheckFromContext(ctx)
	if !ok {
		return tool.Output{}, errors.New("write must execute through ToolExecutionService")
	}
	prepared, ok := check.Prepared.(preparedChange)
	if !ok {
		return tool.Output{}, errors.New("write permission preparation is missing")
	}
	return toolImpl.files.applyPrepared(ctx, invocation, prepared)
}

func (toolImpl readTool) CheckPermissions(_ context.Context, invocation tool.Invocation) (tool.PermissionCheck, error) {
	var args fileReadArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.PermissionCheck{}, err
	}
	resolved, err := toolImpl.files.policy.ResolveExisting(args.Path, project.PathFile)
	if err != nil {
		return tool.PermissionCheck{}, err
	}
	return tool.PermissionCheck{Decision: tool.PermissionAllow, Prepared: preparedRead{args: args, resolved: resolved}}, nil
}

func (toolImpl editTool) CheckPermissions(_ context.Context, invocation tool.Invocation) (tool.PermissionCheck, error) {
	return toolImpl.files.prepareChange(invocation, "edit")
}

func (toolImpl writeTool) CheckPermissions(_ context.Context, invocation tool.Invocation) (tool.PermissionCheck, error) {
	return toolImpl.files.prepareChange(invocation, "write")
}

func (files *FileTools) readPrepared(ctx context.Context, prepared preparedRead) (tool.Output, error) {
	reader, err := workspace.NewReaderWithPolicy(files.root, files.policy)
	if err != nil {
		return tool.Output{}, err
	}
	start := prepared.args.Line
	if start == 0 {
		start = 1
	}
	result, err := reader.ReadRangePrepared(ctx, prepared.resolved.Canonical, prepared.args.Path, workspace.ReadRangeOptions{StartLine: start, LineLimit: prepared.args.Limit, MaxBytes: int(files.maxBytes), MaxLineBytes: files.maxLineBytes, PrefixLines: true})
	if err != nil {
		return tool.Output{}, err
	}
	content, err := os.ReadFile(prepared.resolved.Canonical)
	if err != nil {
		return tool.Output{}, fmt.Errorf("hash read target: %w", err)
	}
	return tool.Output{ToolName: "read", Text: result.Text, Partial: result.Partial, Metadata: map[string]any{
		"path": prepared.resolved.Canonical, "content_hash": hashBytes(content), "start_line": result.StartLine,
		"end_line": result.EndLine, "next_line": result.NextLine, "lines_returned": result.LinesReturned,
		"total_lines": result.TotalLines, "bytes_returned": result.BytesReturned, "output_truncated": result.OutputTruncated,
	}}, nil
}

func (files *FileTools) prepareChange(invocation tool.Invocation, operation string) (tool.PermissionCheck, error) {
	var before, after []byte
	if operation == "edit" {
		var args fileEditArgs
		if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
			return tool.PermissionCheck{}, err
		}
		if args.OldString == args.NewString {
			return tool.PermissionCheck{}, errors.New("edit old_string and new_string are identical")
		}
		resolved, err := files.policy.ResolveExisting(args.Path, project.PathFile)
		if err != nil {
			return tool.PermissionCheck{}, err
		}
		before, err = os.ReadFile(resolved.Canonical)
		if err != nil {
			return tool.PermissionCheck{}, fmt.Errorf("read edit target: %w", err)
		}
		count := strings.Count(string(before), args.OldString)
		if count == 0 {
			return tool.PermissionCheck{}, fmt.Errorf("edit old_string was not found in %q", args.Path)
		}
		if count > 1 && !args.ReplaceAll {
			return tool.PermissionCheck{}, fmt.Errorf("edit old_string matched %d times in %q; set replace_all or provide more context", count, args.Path)
		}
		after = []byte(strings.Replace(string(before), args.OldString, args.NewString, -1))
		return files.changePermissionCheck(invocation, resolved, before, after, operation)
	}
	var args fileWriteArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.PermissionCheck{}, err
	}
	resolved, err := files.policy.ResolveForWriteTarget(args.Path)
	if err != nil {
		return tool.PermissionCheck{}, err
	}
	before, readErr := os.ReadFile(resolved.Canonical)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return tool.PermissionCheck{}, readErr
	}
	if errors.Is(readErr, os.ErrNotExist) {
		before = nil
	}
	after = []byte(args.Content)
	return files.changePermissionCheck(invocation, resolved, before, after, operation)
}

func (files *FileTools) changePermissionCheck(invocation tool.Invocation, resolved project.ResolvedPath, before, after []byte, operation string) (tool.PermissionCheck, error) {
	change := buildFileChange(resolved.Canonical, before, after, operation)
	prepared := preparedChange{resolved: resolved, before: before, after: after, change: change, operation: operation}
	grant := policy.FileDirectoryGrant(filepath.Dir(resolved.Canonical))
	request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeFile, policy.CommandRiskModerate, fileApprovalReason(operation, resolved.Canonical))
	if err != nil {
		return tool.PermissionCheck{}, err
	}
	request.Path, request.Diff = resolved.Canonical, change
	presentationOperation := operation
	if operation == "write" {
		if _, statErr := os.Stat(resolved.Canonical); errors.Is(statErr, os.ErrNotExist) {
			presentationOperation = "write-new"
		} else {
			presentationOperation = "write-overwrite"
		}
	}
	request.Presentation = policy.FileApprovalPresentation(presentationOperation, resolved.Canonical, change)
	return tool.PermissionCheck{Decision: tool.PermissionAsk, Request: &request, Grant: grant, Prepared: prepared}, nil
}

func (files *FileTools) applyPrepared(ctx context.Context, invocation tool.Invocation, prepared preparedChange) (tool.Output, error) {
	change := prepared.change
	if err := ctx.Err(); err != nil {
		return tool.Output{}, err
	}
	current, readErr := os.ReadFile(prepared.resolved.Canonical)
	if prepared.operation == "write" && readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return tool.Output{}, readErr
	}
	if prepared.operation == "edit" && readErr != nil {
		return tool.Output{}, readErr
	}
	if readErr == nil && hashBytes(current) != change.BeforeHash {
		return tool.Output{}, &staleFileError{Path: prepared.resolved.Canonical}
	}
	if err := atomicWrite(prepared.resolved.Canonical, prepared.after, files.fileMode); err != nil {
		return tool.Output{}, err
	}
	return tool.Output{ToolName: invocation.Call.Name, Text: fmt.Sprintf("updated %s", prepared.resolved.Canonical), Metadata: map[string]any{"change": change}}, nil
}

type staleFileError struct{ Path string }

func (err *staleFileError) Error() string {
	return fmt.Sprintf("file changed since approval: %s", err.Path)
}
func (err *staleFileError) ToolErrorKind() string { return "target_stale" }

func fileApprovalReason(operation, path string) string {
	if operation == "write" {
		return "writing file changes the workspace"
	}
	return "editing file changes the workspace"
}

func buildFileChange(path string, before, after []byte, operation string) FileChange {
	stats := textdiff.ChangedLineStats(before, after)
	return FileChange{Path: path, Operation: operation, BeforeHash: hashBytes(before), AfterHash: hashBytes(after), BeforeBytes: len(before), AfterBytes: len(after), UnifiedDiff: textdiff.ContentDiff(operation, path, "", before, after), Insertions: stats.Insertions, Deletions: stats.Deletions}
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

var _ tool.Tool = readTool{}
var _ tool.Tool = editTool{}
var _ tool.Tool = writeTool{}
