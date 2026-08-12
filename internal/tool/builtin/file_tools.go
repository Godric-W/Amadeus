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
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type FileToolsOptions struct {
	FileSystemPolicy   *project.FileSystemPolicy
	Approvals          policy.ApprovalHandler
	SessionApprovals   *policy.FileApprovalStore
	RunPermissions     *project.PermissionStore
	SessionPermissions *project.PermissionStore
	MaxBytes           int64
	MaxLineBytes       int
	FileMode           os.FileMode
}

type FileTools struct {
	policy             *project.FileSystemPolicy
	approvals          policy.ApprovalHandler
	sessionApprovals   *policy.FileApprovalStore
	runPermissions     *project.PermissionStore
	sessionPermissions *project.PermissionStore
	maxBytes           int64
	maxLineBytes       int
	fileMode           os.FileMode
	root               project.Root
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
	if options.Approvals == nil {
		return nil, errors.New("file tools approval handler is nil")
	}
	if options.SessionApprovals == nil {
		options.SessionApprovals = policy.NewFileApprovalStore()
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
	return &FileTools{root: root, policy: options.FileSystemPolicy, approvals: options.Approvals,
		sessionApprovals: options.SessionApprovals, maxBytes: options.MaxBytes,
		maxLineBytes: options.MaxLineBytes, fileMode: options.FileMode,
		runPermissions: options.RunPermissions, sessionPermissions: options.SessionPermissions}, nil
}

func (files *FileTools) ReadSpec() tool.Spec {
	return readSpec()
}

func (files *FileTools) EditSpec() tool.Spec {
	return editSpec()
}

func (files *FileTools) WriteSpec() tool.Spec {
	return writeSpec()
}

type readTool struct{ files *FileTools }
type editTool struct{ files *FileTools }
type writeTool struct{ files *FileTools }

func (files *FileTools) ReadTool() tool.Handler  { return readTool{files: files} }
func (files *FileTools) EditTool() tool.Handler  { return editTool{files: files} }
func (files *FileTools) WriteTool() tool.Handler { return writeTool{files: files} }

func (handler readTool) Spec() tool.Spec                 { return handler.files.ReadSpec() }
func (handler readTool) SupportsParallelToolCalls() bool { return true }
func (handler readTool) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	return handler.files.read(ctx, invocation)
}

func (handler editTool) Spec() tool.Spec                 { return handler.files.EditSpec() }
func (handler editTool) SupportsParallelToolCalls() bool { return false }
func (handler editTool) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	return handler.files.edit(ctx, invocation)
}

func (handler writeTool) Spec() tool.Spec                 { return handler.files.WriteSpec() }
func (handler writeTool) SupportsParallelToolCalls() bool { return false }
func (handler writeTool) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	return handler.files.write(ctx, invocation)
}

func (files *FileTools) read(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	var args fileReadArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.Output{}, err
	}
	resolved, err := files.policy.ResolveExisting(args.Path, project.PathFile)
	if err != nil {
		return tool.Output{}, err
	}
	reader, err := workspace.NewReaderWithPolicy(files.root, files.policy)
	if err != nil {
		return tool.Output{}, err
	}
	start := args.Line
	if start == 0 {
		start = 1
	}
	result, err := reader.ReadRangePrepared(ctx, resolved.Canonical, args.Path, workspace.ReadRangeOptions{StartLine: start, LineLimit: args.Limit, MaxBytes: int(files.maxBytes), MaxLineBytes: files.maxLineBytes, PrefixLines: true})
	if err != nil {
		return tool.Output{}, err
	}
	content, err := os.ReadFile(resolved.Canonical)
	if err != nil {
		return tool.Output{}, fmt.Errorf("hash read target: %w", err)
	}
	return tool.Output{ToolName: "read", Text: result.Text, Partial: result.Partial, Metadata: map[string]any{
		"path": resolved.Canonical, "content_hash": hashBytes(content), "start_line": result.StartLine,
		"end_line": result.EndLine, "next_line": result.NextLine, "lines_returned": result.LinesReturned,
		"total_lines": result.TotalLines, "bytes_returned": result.BytesReturned, "output_truncated": result.OutputTruncated,
	}}, nil
}

func (files *FileTools) edit(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	var args fileEditArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.Output{}, err
	}
	if args.OldString == args.NewString {
		return tool.Output{}, errors.New("edit old_string and new_string are identical")
	}
	resolved, err := files.policy.ResolveExisting(args.Path, project.PathFile)
	if err != nil {
		return tool.Output{}, err
	}
	before, err := os.ReadFile(resolved.Canonical)
	if err != nil {
		return tool.Output{}, fmt.Errorf("read edit target: %w", err)
	}
	count := strings.Count(string(before), args.OldString)
	if count == 0 {
		return tool.Output{}, fmt.Errorf("edit old_string was not found in %q", args.Path)
	}
	if count > 1 && !args.ReplaceAll {
		return tool.Output{}, fmt.Errorf("edit old_string matched %d times in %q; set replace_all or provide more context", count, args.Path)
	}
	after := strings.Replace(string(before), args.OldString, args.NewString, -1)
	return files.applyChange(ctx, invocation, resolved, before, []byte(after), "edit")
}

func (files *FileTools) write(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	var args fileWriteArgs
	if err := decodeArguments(invocation.Call.Payload, &args); err != nil {
		return tool.Output{}, err
	}
	resolved, err := files.policy.ResolveForWriteTarget(args.Path)
	if err != nil {
		return tool.Output{}, err
	}
	before, readErr := os.ReadFile(resolved.Canonical)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return tool.Output{}, readErr
	}
	if errors.Is(readErr, os.ErrNotExist) {
		before = nil
	}
	return files.applyChange(ctx, invocation, resolved, before, []byte(args.Content), "write")
}

func (files *FileTools) applyChange(ctx context.Context, invocation tool.Invocation, resolved project.ResolvedPath, before, after []byte, operation string) (tool.Output, error) {
	change := buildFileChange(resolved.Canonical, before, after, operation)
	approvedSession := files.sessionApprovals.Allows(resolved.Canonical)
	if !approvedSession {
		args := invocation.Call.Payload
		request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, args, policy.ApprovalPurposeFile, policy.CommandRiskModerate, fileApprovalReason(operation, resolved.Canonical))
		if err != nil {
			return tool.Output{}, err
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
		decision, err := files.approvals.Decide(ctx, request)
		if err != nil {
			return tool.Output{}, err
		}
		if err := decision.Validate(); err != nil {
			return tool.Output{}, err
		}
		if !decision.Allowed() {
			return tool.Output{ToolName: invocation.Call.Name, Text: "file change denied", Metadata: map[string]any{"change": change}}, &toolDeniedError{reason: decision.Reason}
		}
		grantRoot := filepath.Dir(resolved.Canonical)
		// A one-call approval still needs a temporary writable grant so the
		// common FileSystemPolicy can authorize the actual write. The store is
		// owned by the current Turn and is discarded when that Turn ends.
		if files.runPermissions != nil {
			if err := files.runPermissions.GrantWritableRoots([]string{grantRoot}); err != nil {
				return tool.Output{}, fmt.Errorf("grant run file permission: %w", err)
			}
		}
		if decision.Scope == policy.ApprovalSession {
			if files.sessionPermissions != nil {
				if err := files.sessionPermissions.GrantWritableRoots([]string{grantRoot}); err != nil {
					return tool.Output{}, fmt.Errorf("grant session file permission: %w", err)
				}
			}
			files.sessionApprovals.ApproveDirectory(filepath.Dir(resolved.Canonical))
		}
	}
	if _, err := files.policy.ResolveForWrite(resolved.Canonical); err != nil {
		return tool.Output{}, err
	}
	if err := ctx.Err(); err != nil {
		return tool.Output{}, err
	}
	current, readErr := os.ReadFile(resolved.Canonical)
	if operation == "write" && readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return tool.Output{}, readErr
	}
	if operation == "edit" && readErr != nil {
		return tool.Output{}, readErr
	}
	if readErr == nil && hashBytes(current) != change.BeforeHash {
		return tool.Output{}, &staleFileError{Path: resolved.Canonical}
	}
	if err := atomicWrite(resolved.Canonical, after, files.fileMode); err != nil {
		return tool.Output{}, err
	}
	return tool.Output{ToolName: invocation.Call.Name, Text: fmt.Sprintf("updated %s", resolved.Canonical), Metadata: map[string]any{"change": change}}, nil
}

type staleFileError struct{ Path string }

func (err *staleFileError) Error() string {
	return fmt.Sprintf("file changed since approval: %s", err.Path)
}
func (err *staleFileError) ToolErrorKind() string { return "target_stale" }

type toolDeniedError struct{ reason string }

func (err *toolDeniedError) Error() string         { return "file change denied: " + err.reason }
func (err *toolDeniedError) ToolErrorKind() string { return "approval_denied" }

func fileApprovalReason(operation, path string) string {
	if operation == "write" {
		return "writing file changes the workspace"
	}
	return "editing file changes the workspace"
}

func buildFileChange(path string, before, after []byte, operation string) FileChange {
	return FileChange{Path: path, Operation: operation, BeforeHash: hashBytes(before), AfterHash: hashBytes(after), BeforeBytes: len(before), AfterBytes: len(after), UnifiedDiff: unifiedDiff(path, string(before), string(after)), Insertions: lineCount(string(after)) - lineCount(string(before)), Deletions: lineCount(string(before)) - lineCount(string(after))}
}

func hashBytes(value []byte) string    { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func readFileBytes(path string) []byte { value, _ := os.ReadFile(path); return value }
func lineCount(value string) int {
	if value == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(value, "\n"), "\n"))
}
func unifiedDiff(path, before, after string) string {
	return fmt.Sprintf("--- %s\n+++ %s\n@@\n-%s\n+%s\n", path, path, strings.TrimSuffix(before, "\n"), strings.TrimSuffix(after, "\n"))
}

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

var _ tool.Handler = readTool{}
var _ tool.Handler = editTool{}
var _ tool.Handler = writeTool{}
