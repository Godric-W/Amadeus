package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type GrepCodeOptions struct {
	MaxResults       int
	MaxFileBytes     int64
	MaxContextLines  int
	RipgrepPath      string
	DisableRipgrep   bool
	MaxRGOutputBytes int64
}

type GrepCode struct {
	root    project.Root
	guard   *project.PathGuard
	options GrepCodeOptions
	ripgrep string
}

type grepCodeArguments struct {
	Query         string `json:"query"`
	Path          string `json:"path,omitempty"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty"`
	Context       int    `json:"context,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type grepMatch struct {
	Path      string
	Line      int
	Lines     []string
	LineIndex int
	Context   int
}

func NewGrepCode(root project.Root, options GrepCodeOptions) (*GrepCode, error) {
	if root.Path() == "" {
		return nil, errors.New("grep_code project root is empty")
	}
	if options.MaxResults <= 0 || options.MaxFileBytes <= 0 || options.MaxContextLines < 0 {
		return nil, errors.New("grep_code limits are invalid")
	}
	if options.MaxRGOutputBytes <= 0 {
		options.MaxRGOutputBytes = 4 << 20
	}
	ripgrep := ""
	if !options.DisableRipgrep {
		if strings.TrimSpace(options.RipgrepPath) != "" {
			ripgrep = options.RipgrepPath
		} else if discovered, err := exec.LookPath("rg"); err == nil {
			ripgrep = discovered
		}
	}
	guard, err := project.NewPathGuard(root)
	if err != nil {
		return nil, err
	}
	return &GrepCode{root: root, guard: guard, options: options, ripgrep: ripgrep}, nil
}

func (grepCode *GrepCode) Spec() tool.Spec {
	return grepCodeSpec()
}

func (grepCode *GrepCode) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments grepCodeArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if arguments.Query == "" {
		return tool.Result{}, errors.New("grep_code query is empty")
	}
	if arguments.Context < 0 || arguments.Limit < 0 {
		return tool.Result{}, errors.New("grep_code context and limit cannot be negative")
	}
	contextLines := arguments.Context
	if contextLines > grepCode.options.MaxContextLines {
		contextLines = grepCode.options.MaxContextLines
	}
	limit := grepCode.options.MaxResults
	if arguments.Limit > 0 && arguments.Limit < limit {
		limit = arguments.Limit
	}
	caseSensitive := true
	if arguments.CaseSensitive != nil {
		caseSensitive = *arguments.CaseSensitive
	}
	matcher, err := compileGrepMatcher(arguments.Query, arguments.Regex, caseSensitive)
	if err != nil {
		return tool.Result{}, err
	}
	searchPath := arguments.Path
	if strings.TrimSpace(searchPath) == "" {
		searchPath = "."
	}
	absoluteSearchPath, err := grepCode.guard.ResolveExisting(searchPath, project.PathAny)
	if err != nil {
		return tool.Result{}, err
	}
	files, backend, err := grepCode.candidateFiles(ctx, absoluteSearchPath, searchPath, arguments, caseSensitive)
	if err != nil {
		return tool.Result{}, err
	}
	matches := make([]grepMatch, 0, limit+1)
	filesSearched := 0
	filesSkipped := 0
	for _, filePath := range files {
		if err := ctx.Err(); err != nil {
			return tool.Result{}, err
		}
		content, skipped, err := grepCode.readSearchableFile(filePath)
		if err != nil {
			return tool.Result{}, err
		}
		if skipped {
			filesSkipped++
			continue
		}
		filesSearched++
		lines := strings.Split(string(content), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		relative, err := grepCode.root.Relative(filePath)
		if err != nil {
			return tool.Result{}, err
		}
		for lineIndex, line := range lines {
			if matcher.MatchString(line) {
				matches = append(matches, grepMatch{Path: relative, Line: lineIndex + 1, Lines: lines, LineIndex: lineIndex, Context: contextLines})
				if len(matches) > limit {
					break
				}
			}
		}
		if len(matches) > limit {
			break
		}
	}
	partial := len(matches) > limit
	if partial {
		matches = matches[:limit]
	}
	return tool.Result{
		ToolName: "grep_code", Text: formatGrepMatches(matches), Partial: partial,
		Metadata: map[string]any{
			"query": arguments.Query, "path": searchPath, "matches_returned": len(matches),
			"files_searched": filesSearched, "files_skipped": filesSkipped, "backend": backend,
		},
	}, nil
}

func (grepCode *GrepCode) candidateFiles(ctx context.Context, absoluteSearchPath, relativeSearchPath string, arguments grepCodeArguments, caseSensitive bool) ([]string, string, error) {
	if grepCode.ripgrep != "" {
		files, err := grepCode.ripgrepFiles(ctx, relativeSearchPath, arguments.Query, arguments.Regex, caseSensitive)
		if err == nil {
			return files, "rg", nil
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
	}
	files, err := grepCode.collectFiles(ctx, absoluteSearchPath)
	return files, "go", err
}

func (grepCode *GrepCode) ripgrepFiles(ctx context.Context, searchPath, query string, regex, caseSensitive bool) ([]string, error) {
	arguments := []string{"--files-with-matches", "--null", "--color", "never", "--no-messages"}
	if !regex {
		arguments = append(arguments, "--fixed-strings")
	}
	if !caseSensitive {
		arguments = append(arguments, "--ignore-case")
	}
	for directory := range ignoredGlobDirectories {
		arguments = append(arguments, "--glob", "!"+directory+"/**")
	}
	arguments = append(arguments, "--", query, searchPath)
	command := exec.CommandContext(ctx, grepCode.ripgrep, arguments...)
	command.Dir = grepCode.root.Path()
	stdout := &limitedCommandBuffer{limit: grepCode.options.MaxRGOutputBytes}
	stderr := &limitedCommandBuffer{limit: 64 << 10}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.exceeded {
		return nil, errors.New("ripgrep candidate output exceeded limit")
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("run ripgrep: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	parts := bytes.Split(stdout.Bytes(), []byte{0})
	files := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		relative := filepath.ToSlash(string(part))
		resolved, err := grepCode.guard.ResolveExisting(filepath.FromSlash(relative), project.PathFile)
		if err != nil {
			return nil, fmt.Errorf("validate ripgrep result %q: %w", relative, err)
		}
		if _, duplicate := seen[resolved]; duplicate {
			continue
		}
		seen[resolved] = struct{}{}
		files = append(files, resolved)
	}
	sort.Strings(files)
	return files, nil
}

type limitedCommandBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	written  int64
	exceeded bool
}

func (buffer *limitedCommandBuffer) Write(content []byte) (int, error) {
	originalLength := len(content)
	remaining := buffer.limit - buffer.written
	if remaining <= 0 {
		buffer.exceeded = true
		return originalLength, nil
	}
	if int64(len(content)) > remaining {
		content = content[:remaining]
		buffer.exceeded = true
	}
	_, _ = buffer.buffer.Write(content)
	buffer.written += int64(len(content))
	return originalLength, nil
}

func (buffer *limitedCommandBuffer) Bytes() []byte {
	return buffer.buffer.Bytes()
}

func (buffer *limitedCommandBuffer) String() string {
	return buffer.buffer.String()
}

func compileGrepMatcher(query string, regex, caseSensitive bool) (*regexp.Regexp, error) {
	pattern := query
	if !regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile grep_code query: %w", err)
	}
	return matcher, nil
}

func (grepCode *GrepCode) collectFiles(ctx context.Context, searchPath string) ([]string, error) {
	info, err := os.Stat(searchPath)
	if err != nil {
		return nil, fmt.Errorf("stat grep_code path: %w", err)
	}
	if info.Mode().IsRegular() {
		return []string{searchPath}, nil
	}
	if !info.IsDir() {
		return nil, errors.New("grep_code path is not a regular file or directory")
	}
	files := make([]string, 0)
	err = filepath.WalkDir(searchPath, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filePath == searchPath {
			return nil
		}
		if entry.IsDir() {
			if _, ignored := ignoredGlobDirectories[entry.Name()]; ignored || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			relative, err := grepCode.root.Relative(filePath)
			if err != nil {
				return err
			}
			resolved, err := grepCode.guard.ResolveExisting(filepath.FromSlash(relative), project.PathAny)
			if err != nil {
				return err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				files = append(files, resolved)
			}
			return nil
		}
		if entry.Type().IsRegular() {
			files = append(files, filePath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk grep_code path: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func (grepCode *GrepCode) readSearchableFile(filePath string) ([]byte, bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("stat grep_code file: %w", err)
	}
	if info.Size() > grepCode.options.MaxFileBytes {
		return nil, true, nil
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("read grep_code file: %w", err)
	}
	if int64(len(content)) > grepCode.options.MaxFileBytes || !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return nil, true, nil
	}
	return content, false, nil
}

func formatGrepMatches(matches []grepMatch) string {
	blocks := make([]string, 0, len(matches))
	for _, match := range matches {
		start := match.LineIndex - match.Context
		if start < 0 {
			start = 0
		}
		end := match.LineIndex + match.Context + 1
		if end > len(match.Lines) {
			end = len(match.Lines)
		}
		lines := make([]string, 0, end-start)
		for index := start; index < end; index++ {
			separator := "-"
			if index == match.LineIndex {
				separator = ":"
			}
			lines = append(lines, fmt.Sprintf("%s%s%d%s%s", match.Path, separator, index+1, separator, match.Lines[index]))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n--\n")
}

var _ tool.Tool = (*GrepCode)(nil)
