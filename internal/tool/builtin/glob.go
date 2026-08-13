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

func (glob *Glob) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments globArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	pattern, err := workspace.NormalizeGlob(arguments.Pattern)
	if err != nil {
		return tool.Output{}, fmt.Errorf("glob pattern is invalid: %w", err)
	}
	if arguments.Limit < 0 {
		return tool.Output{}, errors.New("glob limit cannot be negative")
	}
	base := strings.TrimSpace(arguments.Path)
	if base == "" {
		base = "."
	}
	resolved, err := glob.reader.ResolveExistingTarget(base, project.PathDirectory)
	if err != nil {
		return tool.Output{}, err
	}
	absoluteBase := resolved.Canonical
	limit := glob.options.MaxResults
	if arguments.Limit > 0 && arguments.Limit < limit {
		limit = arguments.Limit
	}
	matches, partial, backend, err := glob.ripgrepMatches(ctx, absoluteBase, pattern, arguments.IncludeHidden, limit)
	if err != nil && ctx.Err() != nil {
		return tool.Output{}, ctx.Err()
	}
	if err != nil || glob.ripgrep == "" {
		matches, partial, err = glob.goMatches(ctx, absoluteBase, pattern, arguments.IncludeHidden, limit)
		backend = "go"
		if err != nil {
			return tool.Output{}, err
		}
	}
	return tool.Output{
		ToolName: "glob", Text: strings.Join(matches, "\n"), Partial: partial,
		Metadata: map[string]any{"path": base, "pattern": pattern, "matches_returned": len(matches), "backend": backend},
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

var _ tool.Tool = (*Glob)(nil)
