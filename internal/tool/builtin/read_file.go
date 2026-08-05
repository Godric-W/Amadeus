package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type ReadFileOptions struct {
	MaxBytes     int64
	MaxLineBytes int
}

type ReadFile struct {
	reader  *workspace.Reader
	options ReadFileOptions
}

type readFileArguments struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Offset *int   `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func NewReadFile(root project.Root, options ReadFileOptions) (*ReadFile, error) {
	if root.Path() == "" {
		return nil, errors.New("read_file project root is empty")
	}
	if options.MaxBytes <= 0 {
		return nil, errors.New("read_file max bytes must be greater than zero")
	}
	if options.MaxLineBytes <= 0 {
		options.MaxLineBytes = 32 << 10
	}
	reader, err := workspace.NewReader(root)
	if err != nil {
		return nil, err
	}
	return &ReadFile{reader: reader, options: options}, nil
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
	if arguments.Line < 0 || arguments.Limit < 0 || (arguments.Offset != nil && *arguments.Offset < 0) {
		return tool.Result{}, errors.New("read_file line, legacy offset and limit cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	startLine := arguments.Line
	if startLine == 0 {
		startLine = 1
	}
	if arguments.Offset != nil {
		if arguments.Line != 0 {
			return tool.Result{}, errors.New("read_file line and legacy offset cannot be used together")
		}
		startLine = *arguments.Offset + 1
	}
	read, err := readFile.reader.ReadRange(ctx, arguments.Path, workspace.ReadRangeOptions{
		StartLine: startLine, LineLimit: arguments.Limit, MaxBytes: int(readFile.options.MaxBytes),
		MaxLineBytes: readFile.options.MaxLineBytes, PrefixLines: true,
	})
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{
		ToolName: "read_file", Text: read.Text, Partial: read.Partial,
		Metadata: map[string]any{
			"path": arguments.Path, "start_line": read.StartLine, "end_line": read.EndLine,
			"next_line": read.NextLine, "lines_returned": read.LinesReturned, "total_lines": read.TotalLines,
			"bytes_returned": read.BytesReturned, "file_bytes": read.FileBytes,
			"estimated_tokens": workspace.EstimateTokens(read.BytesReturned), "output_truncated": read.OutputTruncated,
			"lines_truncated": read.LinesTruncated,
		},
	}, nil
}

var _ tool.Tool = (*ReadFile)(nil)
