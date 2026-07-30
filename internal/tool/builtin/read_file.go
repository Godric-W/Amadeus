package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ReadFileOptions struct {
	MaxBytes int64
}

type ReadFile struct {
	root    project.Root
	options ReadFileOptions
}

type readFileArguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func NewReadFile(root project.Root, options ReadFileOptions) (*ReadFile, error) {
	if root.Path() == "" {
		return nil, errors.New("read_file project root is empty")
	}
	if options.MaxBytes <= 0 {
		return nil, errors.New("read_file max bytes must be greater than zero")
	}
	return &ReadFile{root: root, options: options}, nil
}

func (readFile *ReadFile) Spec() tool.Spec {
	return readFileSpec()
}

func (readFile *ReadFile) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments readFileArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(arguments.Path) == "" {
		return tool.Result{}, errors.New("read_file path is empty")
	}
	if arguments.Offset < 0 || arguments.Limit < 0 {
		return tool.Result{}, errors.New("read_file offset and limit cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	path, err := readFile.root.Resolve(arguments.Path)
	if err != nil {
		return tool.Result{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return tool.Result{}, fmt.Errorf("open file %q: %w", arguments.Path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return tool.Result{}, fmt.Errorf("stat file %q: %w", arguments.Path, err)
	}
	if !info.Mode().IsRegular() {
		return tool.Result{}, fmt.Errorf("read_file path is not a regular file: %q", arguments.Path)
	}
	if info.Size() > readFile.options.MaxBytes {
		return tool.Result{}, fmt.Errorf("read_file size %d exceeds limit %d", info.Size(), readFile.options.MaxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, readFile.options.MaxBytes+1))
	if err != nil {
		return tool.Result{}, fmt.Errorf("read file %q: %w", arguments.Path, err)
	}
	if int64(len(content)) > readFile.options.MaxBytes {
		return tool.Result{}, fmt.Errorf("read_file size exceeds limit %d", readFile.options.MaxBytes)
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	if bytesAreBinary(content) {
		return tool.Result{}, fmt.Errorf("read_file does not support binary or non-UTF-8 file %q", arguments.Path)
	}

	lines := splitLines(string(content))
	start := arguments.Offset
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if arguments.Limit > 0 && start+arguments.Limit < end {
		end = start + arguments.Limit
	}
	selected := strings.Join(lines[start:end], "")
	partial := start > 0 || end < len(lines)
	return tool.Result{
		ToolName: "read_file", Text: selected, Partial: partial,
		Metadata: map[string]any{
			"path": arguments.Path, "offset": start, "lines_returned": end - start,
			"total_lines": len(lines), "bytes": len(content),
		},
	}, nil
}

func bytesAreBinary(content []byte) bool {
	return !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

var _ tool.Tool = (*ReadFile)(nil)
