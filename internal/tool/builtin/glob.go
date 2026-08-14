package builtin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type GlobOptions struct {
	MaxResults       int
	MaxOutputBytes   int
	MaxOutputTokens  int
	RipgrepPath      string
	DisableRipgrep   bool
	MaxRGOutputBytes int64
	FileSystemPolicy *project.FileSystemPolicy
}

type Glob struct {
	root       project.Root
	reader     *workspace.Reader
	enumerator *workspace.FileEnumerator
	options    GlobOptions
	ripgrep    string
}

type globArguments struct {
	Path          string `json:"path,omitempty"`
	Pattern       string `json:"pattern"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type preparedGlob struct {
	arguments    globArguments
	pattern      string
	absoluteBase string
	displayBase  string
	limit        int
}

var ignoredGlobDirectories = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {}, "node_modules": {}, "vendor": {}, "dist": {}, "build": {}, "target": {},
}

func NewGlob(root project.Root, options GlobOptions) (*Glob, error) {
	if root.Path() == "" {
		return nil, errors.New("glob project root is empty")
	}
	if options.MaxResults <= 0 {
		return nil, errors.New("glob max results must be greater than zero")
	}
	if options.MaxRGOutputBytes <= 0 {
		options.MaxRGOutputBytes = 4 << 20
	}
	if options.MaxOutputBytes <= 0 {
		options.MaxOutputBytes = 256 << 10
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = 64_000
	}
	reader, err := workspace.NewReaderWithPolicy(root, options.FileSystemPolicy)
	if options.FileSystemPolicy == nil {
		reader, err = workspace.NewReader(root)
	}
	if err != nil {
		return nil, err
	}
	matcher, err := workspace.LoadIgnoreMatcher(root)
	if err != nil {
		return nil, fmt.Errorf("load workspace ignores: %w", err)
	}
	enumerator, err := workspace.NewFileEnumeratorWithPolicy(root, matcher, options.FileSystemPolicy)
	if options.FileSystemPolicy == nil {
		enumerator, err = workspace.NewFileEnumerator(root, matcher)
	}
	if err != nil {
		return nil, err
	}
	ripgrep := ""
	if !options.DisableRipgrep {
		if strings.TrimSpace(options.RipgrepPath) != "" {
			ripgrep = options.RipgrepPath
		} else if discovered, lookErr := exec.LookPath("rg"); lookErr == nil {
			ripgrep = discovered
		}
	}
	return &Glob{root: root, reader: reader, enumerator: enumerator, options: options, ripgrep: ripgrep}, nil
}

func (glob *Glob) Spec() tool.ToolSpec {
	return globSpec()
}

func (glob *Glob) SupportsParallelToolCalls() bool { return true }

func (glob *Glob) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments globArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if _, err := workspace.NormalizeGlob(arguments.Pattern); err != nil {
		return fmt.Errorf("glob pattern is invalid: %w", err)
	}
	if arguments.Limit < 0 {
		return errors.New("glob limit cannot be negative")
	}
	return nil
}

func (glob *Glob) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments globArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	pattern, err := workspace.NormalizeGlob(arguments.Pattern)
	if err != nil {
		return tool.PreparedToolUse{}, fmt.Errorf("glob pattern is invalid: %w", err)
	}
	base := strings.TrimSpace(arguments.Path)
	if base == "" {
		base = "."
	}
	resolved, err := glob.reader.ResolveExistingTarget(base, project.PathDirectory)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	absoluteBase := resolved.Canonical
	limit := glob.options.MaxResults
	if arguments.Limit > 0 && arguments.Limit < limit {
		limit = arguments.Limit
	}
	state := preparedGlob{arguments: arguments, pattern: pattern, absoluteBase: absoluteBase, displayBase: base, limit: limit}
	target := tool.ContextTarget{Path: resolved.Canonical, Kind: tool.ContextTargetDirectory, SideEffect: tool.SideEffectRead}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: state, Target: &target, Permission: tool.AllowPermission()}, nil
}

func (glob *Glob) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedGlob)
	if !ok {
		return tool.ToolResult{}, errors.New("glob preparation state is invalid")
	}
	matches, partial, backend, err := glob.ripgrepMatches(toolContext.Context, state.absoluteBase, state.pattern, state.arguments.IncludeHidden, state.limit)
	if err != nil && toolContext.Context.Err() != nil {
		return tool.ToolResult{}, toolContext.Context.Err()
	}
	if err != nil || glob.ripgrep == "" {
		matches, partial, err = glob.goMatches(toolContext.Context, state.absoluteBase, state.pattern, state.arguments.IncludeHidden, state.limit)
		backend = "go"
		if err != nil {
			return tool.ToolResult{}, err
		}
	}
	matches, text, outputPartial, outputReason := boundRenderedItems(matches, func(items []string) string { return strings.Join(items, "\n") }, glob.options.MaxOutputBytes, glob.options.MaxOutputTokens)
	reason := tool.TruncationNone
	partial = partial || outputPartial
	if outputPartial {
		reason = outputReason
	} else if partial {
		reason = tool.TruncationResultLimit
	}
	data := tool.BoundedResult[string]{Items: matches, Truncated: partial, Reason: reason, Bytes: len(text), TokenEstimate: (len(text) + 3) / 4}
	return tool.ToolResult{
		ToolName: "glob", Text: strings.Join(matches, "\n"), Partial: partial,
		Data: data, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: "Files", Summary: fmt.Sprintf("%d matches", len(matches))},
		Metadata: map[string]any{"path": state.displayBase, "pattern": state.pattern, "matches_returned": len(matches), "backend": backend, "truncation_reason": reason},
	}, nil
}

func (glob *Glob) ripgrepMatches(ctx context.Context, absoluteBase, pattern string, includeHidden bool, limit int) ([]string, bool, string, error) {
	if glob.ripgrep == "" {
		return nil, false, "", errors.New("ripgrep is unavailable")
	}
	baseRelative, err := glob.root.Relative(absoluteBase)
	if err != nil {
		return nil, false, "", err
	}
	arguments := []string{"--files", "--null", "--no-messages"}
	if includeHidden {
		arguments = append(arguments, "--hidden")
	}
	for directory := range ignoredGlobDirectories {
		arguments = append(arguments, "--glob", "!"+directory+"/**")
	}
	arguments = append(arguments, "--", filepath.FromSlash(baseRelative))
	command := exec.CommandContext(ctx, glob.ripgrep, arguments...)
	command.Dir = glob.root.Path()
	stdout := &limitedCommandBuffer{limit: glob.options.MaxRGOutputBytes}
	stderr := &limitedCommandBuffer{limit: 64 << 10}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return nil, false, "", fmt.Errorf("run ripgrep files: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.exceeded {
		return nil, false, "", errors.New("ripgrep file output exceeded limit")
	}
	matches := make([]string, 0, limit+1)
	for _, part := range bytes.Split(stdout.Bytes(), []byte{0}) {
		if len(part) == 0 {
			continue
		}
		relative := strings.TrimPrefix(filepath.ToSlash(string(part)), "./")
		if _, err := glob.reader.ResolveExisting(filepath.FromSlash(relative), project.PathFile); err != nil {
			return nil, false, "", err
		}
		candidate, err := filepath.Rel(absoluteBase, filepath.Join(glob.root.Path(), filepath.FromSlash(relative)))
		if err != nil {
			return nil, false, "", err
		}
		matched, err := workspace.MatchGlob(pattern, filepath.ToSlash(candidate))
		if err != nil {
			return nil, false, "", err
		}
		if matched {
			matches = append(matches, relative)
			if len(matches) > limit {
				break
			}
		}
	}
	sort.Strings(matches)
	partial := len(matches) > limit
	if partial {
		matches = matches[:limit]
	}
	return matches, partial, "rg", nil
}

func (glob *Glob) goMatches(ctx context.Context, absoluteBase, pattern string, includeHidden bool, limit int) ([]string, bool, error) {
	result, err := glob.enumerator.EnumeratePrepared(ctx, absoluteBase, workspace.EnumerateOptions{IncludeHidden: includeHidden, MaxResults: 0})
	if err != nil {
		return nil, false, fmt.Errorf("enumerate project files: %w", err)
	}
	matches := make([]string, 0, limit+1)
	for _, entry := range result.Files {
		candidate, err := filepath.Rel(absoluteBase, entry.Absolute)
		if err != nil {
			return nil, false, err
		}
		matched, err := workspace.MatchGlob(pattern, filepath.ToSlash(candidate))
		if err != nil {
			return nil, false, err
		}
		if matched {
			matches = append(matches, entry.Relative)
			if len(matches) > limit {
				break
			}
		}
	}
	sort.Strings(matches)
	partial := len(matches) > limit
	if partial {
		matches = matches[:limit]
	}
	return matches, partial, nil
}

var _ tool.ToolDefinition = (*Glob)(nil)
