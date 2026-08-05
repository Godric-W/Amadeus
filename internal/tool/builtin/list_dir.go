package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type ListDirOptions struct {
	MaxEntries int
}

type ListDir struct {
	reader  *workspace.Reader
	options ListDirOptions
}

type listDirArguments struct {
	Path          string `json:"path,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

func NewListDir(root project.Root, options ListDirOptions) (*ListDir, error) {
	if root.Path() == "" {
		return nil, errors.New("list_dir project root is empty")
	}
	if options.MaxEntries <= 0 {
		return nil, errors.New("list_dir max entries must be greater than zero")
	}
	reader, err := workspace.NewReader(root)
	if err != nil {
		return nil, err
	}
	return &ListDir{reader: reader, options: options}, nil
}

func (listDir *ListDir) Spec() tool.Spec {
	return listDirSpec()
}

func (listDir *ListDir) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments listDirArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if arguments.Limit < 0 {
		return tool.Result{}, errors.New("list_dir limit cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	relativePath := arguments.Path
	if strings.TrimSpace(relativePath) == "" {
		relativePath = "."
	}
	path, err := listDir.reader.ResolveExisting(relativePath, project.PathDirectory)
	if err != nil {
		return tool.Result{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return tool.Result{}, fmt.Errorf("list directory %q: %w", relativePath, err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	limit := listDir.options.MaxEntries
	if arguments.Limit > 0 && arguments.Limit < limit {
		limit = arguments.Limit
	}
	lines := make([]string, 0, limit)
	totalVisible := 0
	hiddenOmitted := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return tool.Result{}, err
		}
		if !arguments.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
			hiddenOmitted++
			continue
		}
		totalVisible++
		if len(lines) >= limit {
			continue
		}
		line, err := directoryEntryLine(entry)
		if err != nil {
			return tool.Result{}, fmt.Errorf("inspect directory entry %q: %w", entry.Name(), err)
		}
		lines = append(lines, line)
	}
	partial := totalVisible > len(lines)
	return tool.Result{
		ToolName: "list_dir", Text: strings.Join(lines, "\n"), Partial: partial,
		Metadata: map[string]any{
			"path": relativePath, "entries_returned": len(lines), "total_entries": totalVisible,
			"hidden_omitted": hiddenOmitted,
		},
	}, nil
}

func directoryEntryLine(entry os.DirEntry) (string, error) {
	if entry.IsDir() {
		return "dir\t" + entry.Name() + "/", nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return "symlink\t" + entry.Name(), nil
	}
	info, err := entry.Info()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("file\t%s\t%d", entry.Name(), info.Size()), nil
}

var _ tool.Tool = (*ListDir)(nil)
