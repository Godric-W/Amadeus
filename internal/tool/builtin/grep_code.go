package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type GrepCodeOptions struct {
	MaxResults       int
	MaxFileBytes     int64
	MaxContextLines  int
	RipgrepPath      string
	DisableRipgrep   bool
	MaxRGOutputBytes int64
	PathGuard        *project.PathGuard
}

type GrepCode struct {
	root       project.Root
	reader     *workspace.Reader
	enumerator *workspace.FileEnumerator
	options    GrepCodeOptions
	ripgrep    string
}

type grepCodeArguments struct {
	Query         string `json:"query"`
	Path          string `json:"path,omitempty"`
	Glob          string `json:"glob,omitempty"`
	Type          string `json:"type,omitempty"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty"`
	Context       int    `json:"context,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type preparedGrepCode struct {
	arguments     grepCodeArguments
	searchPath    string
	absolutePath  string
	matcher       *regexp.Regexp
	contextLines  int
	limit         int
	caseSensitive bool
}

type grepLine struct {
	Path    string
	Line    int
	Column  int
	Text    string
	Context bool
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
	return &GrepCode{root: root, reader: reader, enumerator: enumerator, options: options, ripgrep: ripgrep}, nil
}

func (grepCode *GrepCode) Spec() tool.Spec { return grepCodeSpec() }

func (grepCode *GrepCode) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments grepCodeArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if arguments.Query == "" {
		return tool.PreparedCall{}, errors.New("grep_code query is empty")
	}
	if arguments.Context < 0 || arguments.Limit < 0 {
		return tool.PreparedCall{}, errors.New("grep_code context and limit cannot be negative")
	}
	if arguments.Glob != "" {
		if _, err := workspace.NormalizeGlob(arguments.Glob); err != nil {
			return tool.PreparedCall{}, fmt.Errorf("grep_code glob is invalid: %w", err)
		}
	}
	contextLines := min(arguments.Context, grepCode.options.MaxContextLines)
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
		return tool.PreparedCall{}, err
	}
	searchPath := strings.TrimSpace(arguments.Path)
	if searchPath == "" {
		searchPath = "."
	}
	resolved, err := grepCode.reader.ResolveExistingTarget(searchPath, project.PathAny)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{preparedFilesystemTarget(resolved)}, Payload: preparedGrepCode{arguments: arguments, searchPath: searchPath, absolutePath: resolved.Canonical, matcher: matcher, contextLines: contextLines, limit: limit, caseSensitive: caseSensitive}})
}

func (grepCode *GrepCode) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	payload, err := preparedPayload[preparedGrepCode](prepared, "grep_code")
	if err != nil {
		return tool.Result{}, err
	}
	arguments, searchPath := payload.arguments, payload.searchPath
	lines, matches, files, skipped, partial, backend, err := grepCode.ripgrepSearch(ctx, payload.absolutePath, arguments, payload.contextLines, payload.limit, payload.caseSensitive)
	if err != nil && ctx.Err() != nil {
		return tool.Result{}, ctx.Err()
	}
	if err != nil || grepCode.ripgrep == "" {
		lines, matches, files, skipped, partial, err = grepCode.goSearch(ctx, payload.absolutePath, arguments, payload.matcher, payload.contextLines, payload.limit)
		backend = "go"
		if err != nil {
			return tool.Result{}, err
		}
	}
	return tool.Result{
		ToolName: "grep_code", Text: formatGrepLines(lines), Partial: partial,
		Metadata: map[string]any{
			"query": arguments.Query, "path": searchPath, "glob": arguments.Glob, "type": arguments.Type,
			"matches_returned": matches, "files_searched": files, "files_skipped": skipped, "backend": backend,
		},
	}, nil
}

type rgJSONEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

func (grepCode *GrepCode) ripgrepSearch(ctx context.Context, searchPath string, arguments grepCodeArguments, contextLines, limit int, caseSensitive bool) ([]grepLine, int, int, int, bool, string, error) {
	if grepCode.ripgrep == "" {
		return nil, 0, 0, 0, false, "", errors.New("ripgrep is unavailable")
	}
	commandArguments := []string{"--json", "--color", "never", "--no-messages", "--line-number", "--column", "--context", fmt.Sprint(contextLines)}
	if !arguments.Regex {
		commandArguments = append(commandArguments, "--fixed-strings")
	}
	if !caseSensitive {
		commandArguments = append(commandArguments, "--ignore-case")
	}
	if arguments.Glob != "" {
		commandArguments = append(commandArguments, "--glob", arguments.Glob)
	}
	if strings.TrimSpace(arguments.Type) != "" {
		commandArguments = append(commandArguments, "--type", arguments.Type)
	}
	for directory := range ignoredGlobDirectories {
		commandArguments = append(commandArguments, "--glob", "!"+directory+"/**")
	}
	commandArguments = append(commandArguments, "--", arguments.Query, searchPath)
	command := exec.CommandContext(ctx, grepCode.ripgrep, commandArguments...)
	command.Dir = grepCode.root.Path()
	stdout := &limitedCommandBuffer{limit: grepCode.options.MaxRGOutputBytes}
	stderr := &limitedCommandBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if ctx.Err() != nil {
		return nil, 0, 0, 0, false, "", ctx.Err()
	}
	if stdout.exceeded {
		return nil, 0, 0, 0, false, "", errors.New("ripgrep output exceeded limit")
	}
	if err != nil {
		var exitError *exec.ExitError
		if !(errors.As(err, &exitError) && exitError.ExitCode() == 1) {
			return nil, 0, 0, 0, false, "", fmt.Errorf("run ripgrep: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	lines := make([]grepLine, 0)
	matches := 0
	recognized := 0
	files := map[string]struct{}{}
	partial := false
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	scanner.Buffer(make([]byte, 64<<10), int(grepCode.options.MaxRGOutputBytes))
	for scanner.Scan() {
		var event rgJSONEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || (event.Type != "match" && event.Type != "context") {
			continue
		}
		recognized++
		path := strings.TrimPrefix(filepath.ToSlash(event.Data.Path.Text), "./")
		if _, err := grepCode.reader.ResolveExisting(filepath.FromSlash(path), project.PathFile); err != nil {
			return nil, 0, 0, 0, false, "", err
		}
		isMatch := event.Type == "match"
		if isMatch {
			matches++
			if matches > limit {
				partial = true
				continue
			}
		}
		if matches > limit {
			continue
		}
		column := 0
		if isMatch && len(event.Data.Submatches) > 0 {
			column = event.Data.Submatches[0].Start + 1
		}
		files[path] = struct{}{}
		lines = append(lines, grepLine{Path: path, Line: event.Data.LineNumber, Column: column, Text: strings.TrimSuffix(event.Data.Lines.Text, "\n"), Context: !isMatch})
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, 0, 0, false, "", err
	}
	if len(stdout.Bytes()) > 0 && recognized == 0 {
		return nil, 0, 0, 0, false, "", errors.New("ripgrep returned no parseable JSON events")
	}
	if matches > limit {
		matches = limit
	}
	return lines, matches, len(files), 0, partial, "rg", nil
}

func (grepCode *GrepCode) goSearch(ctx context.Context, absolutePath string, arguments grepCodeArguments, matcher *regexp.Regexp, contextLines, limit int) ([]grepLine, int, int, int, bool, error) {
	enumerated, err := grepCode.enumerator.EnumeratePrepared(ctx, absolutePath, workspace.EnumerateOptions{})
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	lines := make([]grepLine, 0)
	matches := 0
	filesSearched, filesSkipped := 0, 0
	for _, file := range enumerated.Files {
		if arguments.Glob != "" {
			matched, err := workspace.MatchGlob(arguments.Glob, file.Relative)
			if err != nil || !matched {
				continue
			}
		}
		if !matchesType(file.Relative, arguments.Type) {
			continue
		}
		if file.Size > grepCode.options.MaxFileBytes {
			filesSkipped++
			continue
		}
		content, err := os.ReadFile(file.Absolute)
		if err != nil {
			return nil, 0, 0, 0, false, err
		}
		if int64(len(content)) > grepCode.options.MaxFileBytes || !(workspace.TextDetector{}).Valid(content) {
			filesSkipped++
			continue
		}
		filesSearched++
		fileLines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
		for index, line := range fileLines {
			location := matcher.FindStringIndex(line)
			if location == nil {
				continue
			}
			matches++
			if matches > limit {
				return lines, limit, filesSearched, filesSkipped, true, nil
			}
			start, end := max(0, index-contextLines), min(len(fileLines), index+contextLines+1)
			for current := start; current < end; current++ {
				lines = append(lines, grepLine{Path: file.Relative, Line: current + 1, Column: location[0] + 1, Text: fileLines[current], Context: current != index})
			}
		}
	}
	return lines, matches, filesSearched, filesSkipped, false, nil
}

func matchesType(pathValue, typeName string) bool {
	if strings.TrimSpace(typeName) == "" {
		return true
	}
	extensions := map[string][]string{
		"go": {".go"}, "js": {".js", ".jsx"}, "ts": {".ts", ".tsx"}, "py": {".py"}, "rust": {".rs"},
		"java": {".java"}, "json": {".json"}, "yaml": {".yaml", ".yml"}, "md": {".md", ".markdown"},
	}
	values, exists := extensions[strings.ToLower(strings.TrimSpace(typeName))]
	if !exists {
		return false
	}
	extension := strings.ToLower(filepath.Ext(pathValue))
	for _, candidate := range values {
		if extension == candidate {
			return true
		}
	}
	return false
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

func formatGrepLines(lines []grepLine) string {
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		separator := ":"
		column := line.Column
		if line.Context {
			separator = "-"
			column = 0
		}
		if column > 0 {
			values = append(values, fmt.Sprintf("%s%s%d%s%d%s%s", line.Path, separator, line.Line, separator, column, separator, line.Text))
		} else {
			values = append(values, fmt.Sprintf("%s%s%d%s%s", line.Path, separator, line.Line, separator, line.Text))
		}
	}
	return strings.Join(values, "\n")
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
func (buffer *limitedCommandBuffer) Bytes() []byte  { return buffer.buffer.Bytes() }
func (buffer *limitedCommandBuffer) String() string { return buffer.buffer.String() }

var _ tool.Tool = (*GrepCode)(nil)
