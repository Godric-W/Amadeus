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

type GlobFilesOptions struct {
	MaxResults       int
	RipgrepPath      string
	DisableRipgrep   bool
	MaxRGOutputBytes int64
	PathGuard        *project.PathGuard
}

type GlobFiles struct {
	root       project.Root
	reader     *workspace.Reader
	enumerator *workspace.FileEnumerator
	options    GlobFilesOptions
	ripgrep    string
}

type globFilesArguments struct {
	Path          string `json:"path,omitempty"`
	Pattern       string `json:"pattern"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type preparedGlobFiles struct {
	arguments globFilesArguments
	base      string
	absolute  string
	pattern   string
}

var ignoredGlobDirectories = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {}, "node_modules": {}, "vendor": {}, "dist": {}, "build": {}, "target": {},
}

func NewGlobFiles(root project.Root, options GlobFilesOptions) (*GlobFiles, error) {
	if root.Path() == "" {
		return nil, errors.New("glob_files project root is empty")
	}
	if options.MaxResults <= 0 {
		return nil, errors.New("glob_files max results must be greater than zero")
	}
	if options.MaxRGOutputBytes <= 0 {
		options.MaxRGOutputBytes = 4 << 20
	}
	reader, err := workspace.NewReaderWithGuard(root, options.PathGuard)
	if options.PathGuard == nil {
		reader, err = workspace.NewReader(root)
	}
	if err != nil {
		return nil, err
	}
	matcher, err := workspace.LoadIgnoreMatcher(root)
	if err != nil {
		return nil, fmt.Errorf("load workspace ignores: %w", err)
	}
	enumerator, err := workspace.NewFileEnumeratorWithGuard(root, matcher, options.PathGuard)
	if options.PathGuard == nil {
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
	return &GlobFiles{root: root, reader: reader, enumerator: enumerator, options: options, ripgrep: ripgrep}, nil
}

func (globFiles *GlobFiles) Spec() tool.Spec {
	return globFilesSpec()
}

func (globFiles *GlobFiles) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments globFilesArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	pattern, err := workspace.NormalizeGlob(arguments.Pattern)
	if err != nil {
		return tool.PreparedCall{}, fmt.Errorf("glob_files pattern is invalid: %w", err)
	}
	if arguments.Limit < 0 {
		return tool.PreparedCall{}, errors.New("glob_files limit cannot be negative")
	}
	base := strings.TrimSpace(arguments.Path)
	if base == "" {
		base = "."
	}
	resolved, err := globFiles.reader.ResolveExistingTarget(base, project.PathDirectory)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{preparedFilesystemTarget(resolved)}, Payload: preparedGlobFiles{arguments: arguments, base: base, absolute: resolved.Canonical, pattern: pattern}})
}

func (globFiles *GlobFiles) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	payload, err := preparedPayload[preparedGlobFiles](prepared, "glob_files")
	if err != nil {
		return tool.Result{}, err
	}
	arguments, base, absoluteBase, pattern := payload.arguments, payload.base, payload.absolute, payload.pattern
	limit := globFiles.options.MaxResults
	if arguments.Limit > 0 && arguments.Limit < limit {
		limit = arguments.Limit
	}
	matches, partial, backend, err := globFiles.ripgrepMatches(ctx, absoluteBase, pattern, arguments.IncludeHidden, limit)
	if err != nil && ctx.Err() != nil {
		return tool.Result{}, ctx.Err()
	}
	if err != nil || globFiles.ripgrep == "" {
		matches, partial, err = globFiles.goMatches(ctx, absoluteBase, pattern, arguments.IncludeHidden, limit)
		backend = "go"
		if err != nil {
			return tool.Result{}, err
		}
	}
	return tool.Result{
		ToolName: "glob_files", Text: strings.Join(matches, "\n"), Partial: partial,
		Metadata: map[string]any{"path": base, "pattern": pattern, "matches_returned": len(matches), "backend": backend},
	}, nil
}

func (globFiles *GlobFiles) ripgrepMatches(ctx context.Context, absoluteBase, pattern string, includeHidden bool, limit int) ([]string, bool, string, error) {
	if globFiles.ripgrep == "" {
		return nil, false, "", errors.New("ripgrep is unavailable")
	}
	baseRelative, err := globFiles.root.Relative(absoluteBase)
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
	command := exec.CommandContext(ctx, globFiles.ripgrep, arguments...)
	command.Dir = globFiles.root.Path()
	stdout := &limitedCommandBuffer{limit: globFiles.options.MaxRGOutputBytes}
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
		if _, err := globFiles.reader.ResolveExisting(filepath.FromSlash(relative), project.PathFile); err != nil {
			return nil, false, "", err
		}
		candidate, err := filepath.Rel(absoluteBase, filepath.Join(globFiles.root.Path(), filepath.FromSlash(relative)))
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

func (globFiles *GlobFiles) goMatches(ctx context.Context, absoluteBase, pattern string, includeHidden bool, limit int) ([]string, bool, error) {
	result, err := globFiles.enumerator.EnumeratePrepared(ctx, absoluteBase, workspace.EnumerateOptions{IncludeHidden: includeHidden, MaxResults: 0})
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

var _ tool.Tool = (*GlobFiles)(nil)
