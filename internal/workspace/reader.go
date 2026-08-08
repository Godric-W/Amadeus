package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

type Reader struct {
	root   project.Root
	policy *project.FileSystemPolicy
}

type ReadRangeOptions struct {
	StartLine    int
	LineLimit    int
	MaxBytes     int
	MaxLineBytes int
	PrefixLines  bool
}

type ReadRangeResult struct {
	Text            string
	StartLine       int
	EndLine         int
	NextLine        int
	LinesReturned   int
	TotalLines      int
	BytesReturned   int
	FileBytes       int64
	Partial         bool
	OutputTruncated bool
	LinesTruncated  int
}

func NewReader(root project.Root) (*Reader, error) {
	if root.Path() == "" {
		return nil, errors.New("workspace reader project root is empty")
	}
	policy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: root.Path(), Profile: project.PermissionProfile{WorkspaceRoots: []string{root.Path()}}})
	if err != nil {
		return nil, err
	}
	return &Reader{root: root, policy: policy}, nil
}

func NewReaderWithPolicy(root project.Root, policy *project.FileSystemPolicy) (*Reader, error) {
	if root.Path() == "" {
		return nil, errors.New("workspace reader project root is empty")
	}
	if policy == nil {
		return nil, errors.New("workspace reader filesystem policy is nil")
	}
	return &Reader{root: root, policy: policy}, nil
}

func (reader *Reader) ResolveExisting(relative string, expected project.PathType) (string, error) {
	if reader == nil || reader.policy == nil {
		return "", errors.New("workspace reader is nil")
	}
	resolved, err := reader.policy.ResolveExisting(relative, expected)
	if err != nil {
		return "", err
	}
	return resolved.Canonical, nil
}

func (reader *Reader) ResolveExistingTarget(relative string, expected project.PathType) (project.ResolvedPath, error) {
	if reader == nil || reader.policy == nil {
		return project.ResolvedPath{}, errors.New("workspace reader is nil")
	}
	return reader.policy.ResolveExisting(relative, expected)
}

func (reader *Reader) ReadRange(ctx context.Context, relative string, options ReadRangeOptions) (ReadRangeResult, error) {
	path, err := reader.ResolveExisting(relative, project.PathFile)
	if err != nil {
		return ReadRangeResult{}, err
	}
	return reader.ReadRangePrepared(ctx, path, relative, options)
}

func (reader *Reader) ReadRangePrepared(ctx context.Context, path, displayPath string, options ReadRangeOptions) (ReadRangeResult, error) {
	if options.StartLine <= 0 {
		options.StartLine = 1
	}
	if options.LineLimit < 0 || options.MaxBytes <= 0 || options.MaxLineBytes <= 0 {
		return ReadRangeResult{}, errors.New("workspace read range limits are invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return ReadRangeResult{}, fmt.Errorf("open workspace file %q: %w", displayPath, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ReadRangeResult{}, fmt.Errorf("stat workspace file %q: %w", displayPath, err)
	}

	buffered := bufio.NewReaderSize(file, 64<<10)
	detector := TextDetector{}
	limiter := OutputLimiter{MaxBytes: options.MaxBytes}
	var output strings.Builder
	result := ReadRangeResult{StartLine: options.StartLine, FileBytes: info.Size()}
	lineNumber := 0
	selectionEnded := false
	for {
		if err := ctx.Err(); err != nil {
			return ReadRangeResult{}, err
		}
		line, readErr := buffered.ReadString('\n')
		if line != "" {
			lineNumber++
			if !detector.Valid([]byte(line)) {
				return ReadRangeResult{}, fmt.Errorf("workspace file is binary or non-UTF-8: %q", displayPath)
			}
			selected := lineNumber >= options.StartLine && (options.LineLimit == 0 || result.LinesReturned < options.LineLimit)
			if selected && !selectionEnded {
				formatted := line
				if len(formatted) > options.MaxLineBytes {
					formatted = truncateUTF8(formatted, options.MaxLineBytes) + "…\n"
					result.LinesTruncated++
				}
				if options.PrefixLines {
					formatted = fmt.Sprintf("L%d:%s", lineNumber, formatted)
				}
				_, truncated := limiter.Append(&output, formatted)
				if truncated {
					result.OutputTruncated = true
					selectionEnded = true
				} else {
					result.LinesReturned++
					result.EndLine = lineNumber
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return ReadRangeResult{}, fmt.Errorf("read workspace file %q: %w", displayPath, readErr)
			}
			break
		}
	}
	result.TotalLines = lineNumber
	result.Text = output.String()
	result.BytesReturned = len(result.Text)
	if result.EndLine > 0 && result.EndLine < result.TotalLines {
		result.NextLine = result.EndLine + 1
	}
	result.Partial = options.StartLine > 1 || result.EndLine < result.TotalLines || result.OutputTruncated
	return result, nil
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8RuneStart(value[end]) {
		end--
	}
	return strings.TrimSuffix(value[:end], "\n")
}

func utf8RuneStart(value byte) bool {
	return value&0xC0 != 0x80
}
