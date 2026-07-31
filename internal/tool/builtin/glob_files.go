package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type GlobFilesOptions struct {
	MaxResults int
}

type GlobFiles struct {
	root    project.Root
	guard   *project.PathGuard
	options GlobFilesOptions
}

type globFilesArguments struct {
	Pattern       string `json:"pattern"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	Limit         int    `json:"limit,omitempty"`
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
	guard, err := project.NewPathGuard(root)
	if err != nil {
		return nil, err
	}
	return &GlobFiles{root: root, guard: guard, options: options}, nil
}

func (globFiles *GlobFiles) Spec() tool.Spec {
	return globFilesSpec()
}

func (globFiles *GlobFiles) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments globFilesArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	pattern, err := normalizeGlobPattern(arguments.Pattern)
	if err != nil {
		return tool.Result{}, err
	}
	if arguments.Limit < 0 {
		return tool.Result{}, errors.New("glob_files limit cannot be negative")
	}
	limit := globFiles.options.MaxResults
	if arguments.Limit > 0 && arguments.Limit < limit {
		limit = arguments.Limit
	}
	matches := make([]string, 0, limit+1)
	walkErr := filepath.WalkDir(globFiles.root.Path(), func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filePath == globFiles.root.Path() {
			return nil
		}
		relative, err := globFiles.root.Relative(filePath)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if _, err := globFiles.guard.ResolveExisting(filepath.FromSlash(relative), project.PathAny); err != nil {
				return err
			}
		}
		if entry.IsDir() {
			if _, ignored := ignoredGlobDirectories[entry.Name()]; ignored {
				return filepath.SkipDir
			}
			if !arguments.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !arguments.IncludeHidden && pathHasHiddenSegment(relative) {
			return nil
		}
		matched, err := matchGlob(pattern, relative)
		if err != nil {
			return err
		}
		if matched {
			matches = append(matches, relative)
			if len(matches) > limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return tool.Result{}, fmt.Errorf("glob project files: %w", walkErr)
	}
	sort.Strings(matches)
	partial := len(matches) > limit
	if partial {
		matches = matches[:limit]
	}
	return tool.Result{
		ToolName: "glob_files", Text: strings.Join(matches, "\n"), Partial: partial,
		Metadata: map[string]any{"pattern": pattern, "matches_returned": len(matches)},
	}, nil
}

func normalizeGlobPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	if pattern == "" {
		return "", errors.New("glob_files pattern is empty")
	}
	if path.IsAbs(pattern) {
		return "", errors.New("glob_files pattern must be project-relative")
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == ".." {
			return "", errors.New("glob_files pattern cannot escape project root")
		}
	}
	if _, err := path.Match(strings.ReplaceAll(pattern, "**", "*"), "validation"); err != nil {
		return "", fmt.Errorf("glob_files pattern is invalid: %w", err)
	}
	return pattern, nil
}

func matchGlob(pattern, candidate string) (bool, error) {
	patternSegments := strings.Split(pattern, "/")
	candidateSegments := strings.Split(candidate, "/")
	type position struct{ pattern, candidate int }
	memo := make(map[position]bool)
	seen := make(map[position]bool)
	var match func(int, int) (bool, error)
	match = func(patternIndex, candidateIndex int) (bool, error) {
		key := position{patternIndex, candidateIndex}
		if seen[key] {
			return memo[key], nil
		}
		seen[key] = true
		if patternIndex == len(patternSegments) {
			memo[key] = candidateIndex == len(candidateSegments)
			return memo[key], nil
		}
		if patternSegments[patternIndex] == "**" {
			zero, err := match(patternIndex+1, candidateIndex)
			if err != nil || zero {
				memo[key] = zero
				return zero, err
			}
			if candidateIndex < len(candidateSegments) {
				more, err := match(patternIndex, candidateIndex+1)
				memo[key] = more
				return more, err
			}
			return false, nil
		}
		if candidateIndex >= len(candidateSegments) {
			return false, nil
		}
		segmentMatch, err := path.Match(patternSegments[patternIndex], candidateSegments[candidateIndex])
		if err != nil || !segmentMatch {
			return false, err
		}
		matched, err := match(patternIndex+1, candidateIndex+1)
		memo[key] = matched
		return matched, err
	}
	return match(0, 0)
}

func pathHasHiddenSegment(relative string) bool {
	for _, segment := range strings.Split(relative, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

var _ tool.Tool = (*GlobFiles)(nil)
