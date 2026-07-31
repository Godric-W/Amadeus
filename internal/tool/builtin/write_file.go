package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type WriteFileOptions struct {
	MaxBytes int64
	FileMode os.FileMode
}

type WriteFile struct {
	root    project.Root
	guard   *project.PathGuard
	options WriteFileOptions
}

type writeFileArguments struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewWriteFile(root project.Root, options WriteFileOptions) (*WriteFile, error) {
	if root.Path() == "" {
		return nil, errors.New("write_file project root is empty")
	}
	if options.MaxBytes <= 0 {
		return nil, errors.New("write_file max bytes must be greater than zero")
	}
	if options.FileMode == 0 {
		options.FileMode = 0o644
	}
	guard, err := project.NewPathGuard(root)
	if err != nil {
		return nil, err
	}
	return &WriteFile{root: root, guard: guard, options: options}, nil
}

func (writeFile *WriteFile) Spec() tool.Spec {
	return writeFileSpec()
}

func (writeFile *WriteFile) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments writeFileArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(arguments.Path) == "" {
		return tool.Result{}, errors.New("write_file path is empty")
	}
	if int64(len(arguments.Content)) > writeFile.options.MaxBytes {
		return tool.Result{}, fmt.Errorf("write_file content size %d exceeds limit %d", len(arguments.Content), writeFile.options.MaxBytes)
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	target, err := writeFile.guard.ResolveForWrite(arguments.Path)
	if err != nil {
		return tool.Result{}, err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return tool.Result{}, fmt.Errorf("create parent directory for %q: %w", arguments.Path, err)
	}
	mode := writeFile.options.FileMode
	created := true
	if info, statErr := os.Stat(target); statErr == nil {
		if !info.Mode().IsRegular() {
			return tool.Result{}, fmt.Errorf("write_file path is not a regular file: %q", arguments.Path)
		}
		mode = info.Mode().Perm()
		created = false
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return tool.Result{}, fmt.Errorf("stat write target %q: %w", arguments.Path, statErr)
	}

	temporary, err := os.CreateTemp(parent, ".amadeus-write-*")
	if err != nil {
		return tool.Result{}, fmt.Errorf("create temporary file for %q: %w", arguments.Path, err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(mode); err != nil {
		cleanup()
		return tool.Result{}, fmt.Errorf("set temporary file mode for %q: %w", arguments.Path, err)
	}
	if _, err := temporary.WriteString(arguments.Content); err != nil {
		cleanup()
		return tool.Result{}, fmt.Errorf("write temporary file for %q: %w", arguments.Path, err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return tool.Result{}, fmt.Errorf("sync temporary file for %q: %w", arguments.Path, err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return tool.Result{}, fmt.Errorf("close temporary file for %q: %w", arguments.Path, err)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return tool.Result{}, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		cleanup()
		return tool.Result{}, fmt.Errorf("replace file %q atomically: %w", arguments.Path, err)
	}

	return tool.Result{
		ToolName: "write_file", Text: fmt.Sprintf("wrote %d bytes to %s", len(arguments.Content), arguments.Path),
		Metadata: map[string]any{"path": arguments.Path, "bytes": len(arguments.Content), "created": created, "atomic": true},
	}, nil
}

var _ tool.Tool = (*WriteFile)(nil)
