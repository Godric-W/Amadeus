package patch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/project"
)

type ExecutorOptions struct {
	MaxFileBytes     int64
	FileMode         os.FileMode
	FileSystemPolicy *project.FileSystemPolicy
}

type Executor struct {
	policy    *project.FileSystemPolicy
	options   ExecutorOptions
	commitOps commitOperations
}

type ApplyResult struct {
	Applied []OperationResult
	Partial bool
}

type OperationResult struct {
	Kind        OperationKind
	Path        string
	Destination string
	Bytes       int
	Created     bool
	Deleted     bool
	Moved       bool
	Delta       AppliedPatchDelta
}

type AppliedPatchDelta struct {
	Path        string
	Destination string
	Operation   OperationKind
	OldContent  []byte
	NewContent  []byte
	UnifiedDiff string
	Exact       bool
}

type ConflictError struct {
	Path       string
	Line       int
	Matches    int
	Candidates []int
	Reason     string
	Stale      bool
}

func (conflict *ConflictError) Error() string {
	location := ""
	if conflict.Line > 0 {
		location = fmt.Sprintf(" at patch line %d", conflict.Line)
	}
	candidates := ""
	if len(conflict.Candidates) > 0 {
		candidates = fmt.Sprintf("; candidate lines %v", conflict.Candidates)
	}
	return fmt.Sprintf("patch conflict for %q%s: %s%s", conflict.Path, location, conflict.Reason, candidates)
}

func (conflict *ConflictError) ToolErrorKind() string {
	if conflict != nil && conflict.Stale {
		return "target_stale"
	}
	return "patch_conflict"
}

type commitOperations interface {
	Rename(oldPath, newPath string) error
	Remove(path string) error
}

type osCommitOperations struct{}

func (osCommitOperations) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (osCommitOperations) Remove(path string) error             { return os.Remove(path) }

type preparedOperation struct {
	operation Operation
	target    string
	identity  os.FileInfo
	original  []byte
	content   []byte
	mode      os.FileMode
	temporary string
	source    string
}

type PreparedPatch struct {
	document   Document
	operations []preparedOperation
}

type PreparedTarget struct {
	Requested string
	Canonical string
}

func (prepared *PreparedPatch) Document() Document {
	if prepared == nil {
		return Document{}
	}
	return prepared.document
}

func (prepared *PreparedPatch) Targets() []PreparedTarget {
	if prepared == nil {
		return nil
	}
	targets := make([]PreparedTarget, 0, len(prepared.operations)*2)
	for _, operation := range prepared.operations {
		targets = append(targets, PreparedTarget{Requested: operation.operation.Path, Canonical: operation.source})
		if operation.operation.Kind == OperationMove {
			targets = append(targets, PreparedTarget{Requested: operation.operation.MovePath, Canonical: operation.target})
		}
	}
	return targets
}

func NewExecutor(root project.Root, options ExecutorOptions) (*Executor, error) {
	return newExecutor(root, options, osCommitOperations{})
}

func newExecutor(root project.Root, options ExecutorOptions, commitOps commitOperations) (*Executor, error) {
	if root.Path() == "" {
		return nil, errors.New("apply_patch project root is empty")
	}
	if options.MaxFileBytes <= 0 {
		return nil, errors.New("apply_patch max file bytes must be greater than zero")
	}
	if options.FileMode == 0 {
		options.FileMode = 0o644
	}
	if commitOps == nil {
		return nil, errors.New("apply_patch commit operations are nil")
	}
	fileSystemPolicy := options.FileSystemPolicy
	if fileSystemPolicy == nil {
		var err error
		fileSystemPolicy, err = project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: root.Path(), Profile: project.PermissionProfile{WorkspaceRoots: []string{root.Path()}}})
		if err != nil {
			return nil, err
		}
	}
	return &Executor{policy: fileSystemPolicy, options: options, commitOps: commitOps}, nil
}

func (executor *Executor) Apply(ctx context.Context, document Document) (ApplyResult, error) {
	prepared, err := executor.PreparePatch(ctx, document)
	if err != nil {
		return ApplyResult{}, err
	}
	return executor.ApplyPrepared(ctx, prepared)
}

func (executor *Executor) PreparePatch(ctx context.Context, document Document) (*PreparedPatch, error) {
	if executor == nil || executor.policy == nil {
		return nil, errors.New("apply_patch executor is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateDocument(document); err != nil {
		return nil, err
	}

	prepared := make([]preparedOperation, 0, len(document.Operations))
	for _, operation := range document.Operations {
		candidate, err := executor.prepare(operation)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, candidate)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &PreparedPatch{document: document, operations: prepared}, nil
}

func (executor *Executor) ApplyPrepared(ctx context.Context, document *PreparedPatch) (ApplyResult, error) {
	if executor == nil || document == nil {
		return ApplyResult{}, errors.New("apply_patch prepared document is nil")
	}
	prepared := document.operations
	if err := executor.stage(prepared); err != nil {
		cleanupPrepared(prepared)
		return ApplyResult{}, err
	}
	defer cleanupPrepared(prepared)

	if err := executor.revalidate(prepared); err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{Applied: make([]OperationResult, 0, len(prepared))}
	for index := range prepared {
		if err := ctx.Err(); err != nil {
			result.Partial = len(result.Applied) > 0
			return result, err
		}
		operationResult, applied, err := executor.commit(&prepared[index])
		if applied {
			result.Applied = append(result.Applied, operationResult)
		}
		if err != nil {
			result.Partial = len(result.Applied) > 0
			return result, err
		}
	}
	return result, nil
}

func validateDocument(document Document) error {
	if document.Version != Version1 {
		return fmt.Errorf("apply_patch document version %q is unsupported", document.Version)
	}
	if len(document.Operations) == 0 {
		return errors.New("apply_patch document has no operations")
	}
	seen := make(map[string]struct{}, len(document.Operations))
	for _, operation := range document.Operations {
		if strings.TrimSpace(operation.Path) == "" {
			return errors.New("apply_patch operation path is empty")
		}
		if strings.ContainsRune(operation.Path, '\x00') {
			return fmt.Errorf("apply_patch operation path %q contains NUL", operation.Path)
		}
		if _, duplicate := seen[operation.Path]; duplicate {
			return fmt.Errorf("apply_patch contains duplicate operation for path %q", operation.Path)
		}
		seen[operation.Path] = struct{}{}
		if operation.MovePath != "" {
			if strings.TrimSpace(operation.MovePath) == "" || strings.ContainsRune(operation.MovePath, '\x00') {
				return fmt.Errorf("apply_patch move destination %q is invalid", operation.MovePath)
			}
			if _, duplicate := seen[operation.MovePath]; duplicate {
				return fmt.Errorf("apply_patch contains duplicate operation for path %q", operation.MovePath)
			}
			seen[operation.MovePath] = struct{}{}
		}
		switch operation.Kind {
		case OperationAdd:
			if len(operation.Hunks) != 0 {
				return fmt.Errorf("apply_patch add operation %q cannot contain hunks", operation.Path)
			}
			for _, line := range operation.AddLines {
				if strings.ContainsRune(line, '\x00') {
					return fmt.Errorf("apply_patch add operation %q contains NUL", operation.Path)
				}
			}
		case OperationUpdate:
			if len(operation.Hunks) == 0 {
				return fmt.Errorf("apply_patch update operation %q has no hunks", operation.Path)
			}
			for _, hunk := range operation.Hunks {
				if err := validateHunk(operation.Path, hunk); err != nil {
					return err
				}
			}
		case OperationDelete:
			if len(operation.AddLines) != 0 || len(operation.Hunks) != 0 {
				return fmt.Errorf("apply_patch delete operation %q cannot contain content", operation.Path)
			}
		case OperationMove:
			for _, hunk := range operation.Hunks {
				if err := validateHunk(operation.Path, hunk); err != nil {
					return err
				}
			}
			if operation.MovePath == "" {
				return fmt.Errorf("apply_patch move operation %q has no destination", operation.Path)
			}
		default:
			return fmt.Errorf("apply_patch operation kind %q is invalid", operation.Kind)
		}
	}
	return nil
}

func validateHunk(path string, hunk Hunk) error {
	if len(hunk.Lines) == 0 {
		return fmt.Errorf("apply_patch update operation %q contains empty hunk", path)
	}
	hasOldLine := false
	hasChange := false
	for _, line := range hunk.Lines {
		if strings.ContainsRune(line.Content, '\x00') {
			return fmt.Errorf("apply_patch update operation %q contains NUL", path)
		}
		switch line.Kind {
		case LineContext:
			hasOldLine = true
		case LineAdd:
			hasChange = true
		case LineDelete:
			hasOldLine = true
			hasChange = true
		default:
			return fmt.Errorf("apply_patch update operation %q has invalid line kind %q", path, line.Kind)
		}
	}
	if !hasOldLine {
		return fmt.Errorf("apply_patch update operation %q hunk requires context or deleted lines", path)
	}
	if !hasChange {
		return fmt.Errorf("apply_patch update operation %q hunk has no changes", path)
	}
	return nil
}

func (executor *Executor) prepare(operation Operation) (preparedOperation, error) {
	sourceResolved, err := executor.policy.ResolveForWrite(operation.Path)
	if err != nil {
		return preparedOperation{}, fmt.Errorf("prepare %s %q: %w", operation.Kind, operation.Path, err)
	}
	source := sourceResolved.Canonical
	target := source
	if operation.Kind == OperationMove {
		targetResolved, resolveErr := executor.policy.ResolveForWrite(operation.MovePath)
		err = resolveErr
		if err != nil {
			return preparedOperation{}, fmt.Errorf("prepare move destination %q: %w", operation.MovePath, err)
		}
		target = targetResolved.Canonical
	}
	prepared := preparedOperation{operation: operation, source: source, target: target, mode: executor.options.FileMode}

	switch operation.Kind {
	case OperationAdd:
		if _, err := os.Lstat(target); err == nil {
			return preparedOperation{}, fmt.Errorf("apply_patch add target already exists: %q", operation.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return preparedOperation{}, fmt.Errorf("inspect apply_patch add target %q: %w", operation.Path, err)
		}
		prepared.content = joinAddedLines(operation.AddLines)
	case OperationUpdate, OperationDelete, OperationMove:
		info, err := os.Lstat(source)
		if err != nil {
			return preparedOperation{}, fmt.Errorf("inspect apply_patch %s target %q: %w", operation.Kind, operation.Path, err)
		}
		if !info.Mode().IsRegular() {
			return preparedOperation{}, fmt.Errorf("apply_patch %s target is not a regular file: %q", operation.Kind, operation.Path)
		}
		if info.Size() > executor.options.MaxFileBytes {
			return preparedOperation{}, fmt.Errorf("apply_patch target %q size %d exceeds limit %d",
				operation.Path, info.Size(), executor.options.MaxFileBytes)
		}
		prepared.original, err = os.ReadFile(source)
		if err != nil {
			return preparedOperation{}, fmt.Errorf("read apply_patch target %q: %w", operation.Path, err)
		}
		prepared.mode = info.Mode().Perm()
		prepared.identity = info
		if operation.Kind == OperationMove {
			if _, err := os.Lstat(target); err == nil {
				return preparedOperation{}, fmt.Errorf("apply_patch move destination already exists: %q", operation.MovePath)
			} else if !errors.Is(err, os.ErrNotExist) {
				return preparedOperation{}, fmt.Errorf("inspect apply_patch move destination %q: %w", operation.MovePath, err)
			}
			prepared.content = append([]byte(nil), prepared.original...)
		}
		if operation.Kind == OperationUpdate || (operation.Kind == OperationMove && len(operation.Hunks) > 0) {
			if !validText(prepared.original) {
				return preparedOperation{}, fmt.Errorf("apply_patch target is binary or non-UTF-8: %q", operation.Path)
			}
			prepared.content, err = applyHunks(operation.Path, prepared.original, operation.Hunks)
			if err != nil {
				return preparedOperation{}, err
			}
		}
	}

	if int64(len(prepared.content)) > executor.options.MaxFileBytes {
		return preparedOperation{}, fmt.Errorf("apply_patch result %q size %d exceeds limit %d",
			operation.Path, len(prepared.content), executor.options.MaxFileBytes)
	}
	return prepared, nil
}

func (executor *Executor) stage(prepared []preparedOperation) error {
	for index := range prepared {
		candidate := &prepared[index]
		if candidate.operation.Kind == OperationDelete {
			continue
		}
		parent := filepath.Dir(candidate.target)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create apply_patch parent for %q: %w", candidate.operation.Path, err)
		}
		temporary, err := os.CreateTemp(parent, ".amadeus-patch-*")
		if err != nil {
			return fmt.Errorf("create apply_patch temporary file for %q: %w", candidate.operation.Path, err)
		}
		candidate.temporary = temporary.Name()
		failed := func(err error) error {
			_ = temporary.Close()
			return err
		}
		if err := temporary.Chmod(candidate.mode); err != nil {
			return failed(fmt.Errorf("set apply_patch temporary mode for %q: %w", candidate.operation.Path, err))
		}
		if _, err := temporary.Write(candidate.content); err != nil {
			return failed(fmt.Errorf("write apply_patch temporary file for %q: %w", candidate.operation.Path, err))
		}
		if err := temporary.Sync(); err != nil {
			return failed(fmt.Errorf("sync apply_patch temporary file for %q: %w", candidate.operation.Path, err))
		}
		if err := temporary.Close(); err != nil {
			return fmt.Errorf("close apply_patch temporary file for %q: %w", candidate.operation.Path, err)
		}
	}
	return nil
}

func (executor *Executor) revalidate(prepared []preparedOperation) error {
	for _, candidate := range prepared {
		switch candidate.operation.Kind {
		case OperationAdd:
			if _, err := os.Lstat(candidate.target); err == nil {
				return &ConflictError{Path: candidate.operation.Path, Reason: "add target appeared after preflight", Stale: true}
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("revalidate apply_patch add target %q: %w", candidate.operation.Path, err)
			}
		case OperationUpdate, OperationDelete, OperationMove:
			currentInfo, err := os.Lstat(candidate.source)
			if err != nil || !currentInfo.Mode().IsRegular() || candidate.identity == nil || !os.SameFile(candidate.identity, currentInfo) {
				return &ConflictError{Path: candidate.operation.Path, Reason: "target identity changed or disappeared after preflight", Stale: true}
			}
			current, err := os.ReadFile(candidate.source)
			if err != nil {
				return &ConflictError{Path: candidate.operation.Path, Reason: "target changed or disappeared after preflight", Stale: true}
			}
			if !bytes.Equal(current, candidate.original) {
				return &ConflictError{Path: candidate.operation.Path, Reason: "target changed after preflight", Stale: true}
			}
		}
	}
	return nil
}

func (executor *Executor) commit(candidate *preparedOperation) (OperationResult, bool, error) {
	operation := candidate.operation
	result := OperationResult{Kind: operation.Kind, Path: candidate.source, Destination: candidate.target}
	switch operation.Kind {
	case OperationAdd, OperationUpdate:
		if err := executor.commitOps.Rename(candidate.temporary, candidate.target); err != nil {
			return OperationResult{}, false, fmt.Errorf("commit apply_patch %s %q: %w", operation.Kind, operation.Path, err)
		}
		candidate.temporary = ""
		result.Bytes = len(candidate.content)
		result.Created = operation.Kind == OperationAdd
		result.Delta = newAppliedPatchDelta(operation.Kind, candidate.target, "", candidate.original, candidate.content)
	case OperationDelete:
		if err := executor.commitOps.Remove(candidate.target); err != nil {
			return OperationResult{}, false, fmt.Errorf("commit apply_patch delete %q: %w", operation.Path, err)
		}
		result.Deleted = true
		result.Delta = newAppliedPatchDelta(OperationDelete, candidate.source, "", candidate.original, nil)
	case OperationMove:
		if err := executor.commitOps.Rename(candidate.temporary, candidate.target); err != nil {
			return OperationResult{}, false, fmt.Errorf("commit apply_patch move destination %q: %w", operation.MovePath, err)
		}
		candidate.temporary = ""
		if err := executor.commitOps.Remove(candidate.source); err != nil {
			partial := OperationResult{Kind: OperationAdd, Path: candidate.target, Bytes: len(candidate.content), Created: true,
				Delta: newAppliedPatchDelta(OperationAdd, candidate.target, "", nil, candidate.content)}
			return partial, true, fmt.Errorf("remove apply_patch move source %q after creating destination %q: %w", operation.Path, operation.MovePath, err)
		}
		result.Bytes = len(candidate.content)
		result.Moved = true
		result.Delta = newAppliedPatchDelta(OperationMove, candidate.source, candidate.target, candidate.original, candidate.content)
	}
	return result, true, nil
}

func newAppliedPatchDelta(operation OperationKind, path, destination string, oldContent, newContent []byte) AppliedPatchDelta {
	return AppliedPatchDelta{
		Path: path, Destination: destination, Operation: operation,
		OldContent: append([]byte(nil), oldContent...), NewContent: append([]byte(nil), newContent...),
		UnifiedDiff: unifiedContentDiff(operation, path, destination, oldContent, newContent), Exact: true,
	}
}

func unifiedContentDiff(operation OperationKind, path, destination string, oldContent, newContent []byte) string {
	oldPath, newPath := path, path
	if operation == OperationAdd {
		oldPath = "/dev/null"
	}
	if operation == OperationDelete {
		newPath = "/dev/null"
	}
	if destination != "" {
		newPath = destination
	}
	oldLines, oldFinalNewline := splitDiffLines(oldContent)
	newLines, newFinalNewline := splitDiffLines(newContent)
	oldStart, newStart := 1, 1
	if len(oldLines) == 0 {
		oldStart = 0
	}
	if len(newLines) == 0 {
		newStart = 0
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", oldPath, newPath)
	if !validText(oldContent) || !validText(newContent) {
		builder.WriteString("Binary files differ\n")
		return builder.String()
	}
	fmt.Fprintf(&builder, "@@ -%d,%d +%d,%d @@\n", oldStart, len(oldLines), newStart, len(newLines))
	writeDiffLines(&builder, '-', oldLines, oldFinalNewline)
	writeDiffLines(&builder, '+', newLines, newFinalNewline)
	return builder.String()
}

func writeDiffLines(builder *strings.Builder, prefix byte, lines []string, finalNewline bool) {
	for _, line := range lines {
		builder.WriteByte(prefix)
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	if len(lines) > 0 && !finalNewline {
		builder.WriteString("\\ No newline at end of file\n")
	}
}

func splitDiffLines(content []byte) ([]string, bool) {
	if len(content) == 0 {
		return nil, false
	}
	finalNewline := content[len(content)-1] == '\n'
	lines := strings.Split(string(content), "\n")
	if finalNewline {
		lines = lines[:len(lines)-1]
	}
	return lines, finalNewline
}

func cleanupPrepared(prepared []preparedOperation) {
	for _, candidate := range prepared {
		if candidate.temporary != "" {
			_ = os.Remove(candidate.temporary)
		}
	}
}

func joinAddedLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func validText(content []byte) bool {
	return utf8.Valid(content) && !bytes.Contains(content, []byte{0})
}

func applyHunks(path string, original []byte, hunks []Hunk) ([]byte, error) {
	newline := "\n"
	normalized := string(original)
	if strings.Contains(normalized, "\r\n") {
		withoutCRLF := strings.ReplaceAll(normalized, "\r\n", "")
		if !strings.Contains(withoutCRLF, "\n") {
			newline = "\r\n"
			normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
		}
	}
	hasFinalNewline := strings.HasSuffix(normalized, "\n")
	lines := strings.Split(normalized, "\n")
	if hasFinalNewline {
		lines = lines[:len(lines)-1]
	}

	for _, hunk := range hunks {
		oldLines, newLines := hunkSequences(hunk)
		matches := findSequence(lines, oldLines, hunk.EndOfFile)
		if len(matches) == 0 {
			return nil, &ConflictError{Path: path, Line: hunk.Line, Matches: 0,
				Reason: "hunk context does not match current file"}
		}
		if len(matches) > 1 {
			return nil, &ConflictError{Path: path, Line: hunk.Line, Matches: len(matches), Candidates: oneBased(matches),
				Reason: fmt.Sprintf("hunk context is ambiguous (%d matches)", len(matches))}
		}
		start := matches[0]
		replaced := make([]string, 0, len(lines)-len(oldLines)+len(newLines))
		replaced = append(replaced, lines[:start]...)
		replaced = append(replaced, newLines...)
		replaced = append(replaced, lines[start+len(oldLines):]...)
		lines = replaced
	}

	result := strings.Join(lines, newline)
	if hasFinalNewline {
		result += newline
	}
	return []byte(result), nil
}

func hunkSequences(hunk Hunk) ([]string, []string) {
	oldLines := make([]string, 0, len(hunk.Lines))
	newLines := make([]string, 0, len(hunk.Lines))
	for _, line := range hunk.Lines {
		if line.Kind != LineAdd {
			oldLines = append(oldLines, line.Content)
		}
		if line.Kind != LineDelete {
			newLines = append(newLines, line.Content)
		}
	}
	return oldLines, newLines
}

func findSequence(lines, sequence []string, endOfFile bool) []int {
	matches := findSequenceWith(lines, sequence, endOfFile, func(value string) string { return value })
	if len(matches) > 0 {
		return matches
	}
	matches = findSequenceWith(lines, sequence, endOfFile, func(value string) string { return strings.TrimRight(value, " \t\r") })
	if len(matches) > 0 {
		return matches
	}
	return findSequenceWith(lines, sequence, endOfFile, normalizeEquivalentPunctuation)
}

func findSequenceWith(lines, sequence []string, endOfFile bool, normalize func(string) string) []int {
	if len(sequence) == 0 || len(sequence) > len(lines) {
		return nil
	}
	matches := make([]int, 0, 1)
	for start := 0; start+len(sequence) <= len(lines); start++ {
		if endOfFile && start+len(sequence) != len(lines) {
			continue
		}
		matched := true
		for offset := range sequence {
			if normalize(lines[start+offset]) != normalize(sequence[offset]) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, start)
		}
	}
	return matches
}

var equivalentPunctuation = strings.NewReplacer(
	"‘", "'", "’", "'", "“", `"`, "”", `"`,
	"–", "-", "—", "-", "−", "-", " ", " ",
)

func normalizeEquivalentPunctuation(value string) string {
	return equivalentPunctuation.Replace(strings.TrimRight(value, " \t\r"))
}

func oneBased(values []int) []int {
	result := make([]int, len(values))
	for index, value := range values {
		result[index] = value + 1
	}
	return result
}
